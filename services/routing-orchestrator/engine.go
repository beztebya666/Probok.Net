package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/greenroute/greenroute/internal/contracts"
	"github.com/greenroute/greenroute/internal/domain"
	"github.com/greenroute/greenroute/internal/geometry"
	"github.com/greenroute/greenroute/internal/ids"
	"github.com/greenroute/greenroute/internal/scoring"
	"github.com/greenroute/greenroute/internal/searchstore"
	"github.com/greenroute/greenroute/internal/telemetry"
)

type engine struct {
	root         context.Context
	config       config
	store        searchstore.Store
	provider     *providerClient
	metrics      *telemetry.Metrics
	scoring      scoring.Config
	capabilityMu sync.RWMutex
	capabilities contracts.ProviderCapabilities
	capabilityOK atomic.Bool
	admission    chan struct{}
	workerWG     sync.WaitGroup
	cancelMu     sync.Mutex
	cancels      map[string]context.CancelFunc
	stats        engineStats
}

var (
	errSearchCapacity          = errors.New("route search capacity exhausted")
	errCapabilitiesUnavailable = errors.New("provider capabilities unavailable")
)

type engineStats struct {
	searches       atomic.Int64
	degraded       atomic.Int64
	lowConfidence  atomic.Int64
	budgetExceeded atomic.Int64
	providerCalls  atomic.Int64
	billingUnits   atomic.Int64
}

func newEngine(root context.Context, cfg config, store searchstore.Store, provider *providerClient, metrics *telemetry.Metrics) *engine {
	maxConcurrent := cfg.MaxConcurrentSearches
	if maxConcurrent < 1 {
		maxConcurrent = 64
	}
	return &engine{
		root: root, config: cfg, store: store, provider: provider, metrics: metrics,
		scoring: scoring.DefaultConfig(), cancels: make(map[string]context.CancelFunc),
		admission: make(chan struct{}, maxConcurrent),
	}
}

func (e *engine) discoverCapabilities(ctx context.Context) error {
	capabilities, err := e.provider.capabilities(ctx)
	if err != nil {
		return err
	}
	if capabilities.ContractVersion != contracts.InternalContractVersion {
		return fmt.Errorf("incompatible provider contract version %q", capabilities.ContractVersion)
	}
	if strings.TrimSpace(capabilities.Provider) == "" || strings.TrimSpace(capabilities.Mode) == "" {
		return fmt.Errorf("provider capabilities omit provider or mode")
	}
	if capabilities.MaxAlternatives < 0 || capabilities.MaxAlternatives > 2 {
		return fmt.Errorf("provider maxAlternatives is outside supported range 0..2")
	}
	if capabilities.MaxWaypoints < 2 || capabilities.MaxWaypoints > 50 {
		return fmt.Errorf("provider maxWaypoints is outside supported range 2..50")
	}
	if !capabilities.RealtimeTraffic || !capabilities.TrafficDisabledBaseline {
		return fmt.Errorf("provider lacks required realtime or traffic-disabled baseline capability")
	}
	e.capabilityMu.Lock()
	e.capabilities = capabilities
	e.capabilityMu.Unlock()
	e.capabilityOK.Store(true)
	return nil
}

func (e *engine) refreshCapabilities(ctx context.Context) {
	nextDelay := time.Duration(0)
	for {
		if nextDelay > 0 {
			timer := time.NewTimer(nextDelay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
		discoveryContext, cancel := context.WithTimeout(ctx, 2*time.Second)
		err := e.discoverCapabilities(discoveryContext)
		cancel()
		if err != nil {
			e.capabilityOK.Store(false)
			slog.Warn("provider capability discovery failed", "error_type", fmt.Sprintf("%T", err))
			nextDelay = time.Second
			continue
		}
		nextDelay = 5 * time.Minute
	}
}

func (e *engine) recoverStaleSearches(ctx context.Context) {
	interval := 5 * time.Second
	for {
		e.recoverStaleBatch(ctx, time.Now().UTC())
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (e *engine) recoverStaleBatch(ctx context.Context, now time.Time) {
	staleAfter := e.config.StaleSearchAfter
	if staleAfter == 0 {
		staleAfter = 45 * time.Second
	}
	searches, err := e.store.ActiveBefore(ctx, now.Add(-staleAfter), 100)
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			slog.Error("stale search discovery failed", "error_type", fmt.Sprintf("%T", err))
		}
		return
	}
	for _, result := range searches {
		result.Status = domain.SearchFailed
		result.Warnings = appendUniqueString(result.Warnings, "WORKER_INTERRUPTED")
		result.GeneratedAt = now
		event := domain.SearchEvent{
			SearchID: result.SearchID, Type: domain.EventSearchFailed, Timestamp: now,
			Message: "Search worker was interrupted and the operation was safely closed", Result: cloneResult(&result),
		}
		if err := e.store.Finalize(ctx, result, event, e.config.StateTTL); err != nil {
			if !errors.Is(err, searchstore.ErrNotFound) {
				slog.Error("stale search finalization failed", "search_id", result.SearchID, "error_type", fmt.Sprintf("%T", err))
				e.metrics.SearchFinalizationFailures.Inc()
			}
			continue
		}
		e.metrics.StaleSearchRecoveries.Inc()
		e.metrics.RouteSearchTotal.WithLabelValues(string(domain.SearchFailed), "recovered").Inc()
		slog.Warn("interrupted route search finalized", "search_id", result.SearchID)
	}
}

func (e *engine) capabilitySnapshot() (contracts.ProviderCapabilities, bool) {
	e.capabilityMu.RLock()
	capabilities := e.capabilities
	e.capabilityMu.RUnlock()
	return capabilities, e.capabilityOK.Load()
}

func (e *engine) start(request domain.RouteSearchRequest, operationKey string) (domain.RouteSearchResult, error) {
	request.ApplySafeDefaults()
	if err := request.Validate(domain.DefaultValidationLimits()); err != nil {
		return domain.RouteSearchResult{}, err
	}
	if request.RequestID == "" {
		request.RequestID = ids.New()
	}
	capabilities, ready := e.capabilitySnapshot()
	if !ready {
		e.metrics.RouteSearchRejected.WithLabelValues("capabilities_unavailable").Inc()
		return domain.RouteSearchResult{}, errCapabilitiesUnavailable
	}
	select {
	case e.admission <- struct{}{}:
	default:
		e.metrics.RouteSearchRejected.WithLabelValues("capacity").Inc()
		return domain.RouteSearchResult{}, errSearchCapacity
	}
	releaseAdmission := true
	defer func() {
		if releaseAdmission {
			<-e.admission
		}
	}()
	now := time.Now().UTC()
	searchID := ids.New()
	if operationKey != "" {
		searchID = ids.FromStableKey("route-search", operationKey)
	}
	result := domain.RouteSearchResult{
		SearchID: searchID, RequestID: request.RequestID, Status: domain.SearchAccepted,
		Alternatives: []domain.RouteCandidate{}, BestEffortRoutes: []domain.RouteCandidate{},
		GreenTopRoutes: []domain.RouteCandidate{},
		Warnings: []string{}, ScoringPolicyVersion: e.scoring.PolicyVersion,
		Constraints: domain.AppliedConstraints{
			MaxExtraDistanceMeters: request.MaxExtraDistanceMeters, MaxExtraDistancePercent: request.MaxExtraDistancePercent,
			MaxExtraTimeSeconds: request.MaxExtraTimeSeconds, MinimumConfidenceScore: e.scoring.MinimumRouteConfidence,
			AvoidTolls: request.AvoidTolls, AvoidUnpaved: request.AvoidUnpaved,
		},
		ProviderUsage: domain.ProviderUsage{Provider: defaultString(capabilities.Provider, "yandex"), RequestBudget: request.MaxProviderRequests},
		GeneratedAt:   now, ExpiresAt: now.Add(e.config.StateTTL),
	}
	if err := e.store.Create(e.root, result, e.config.StateTTL); err != nil {
		if errors.Is(err, searchstore.ErrAlreadyExists) && operationKey != "" {
			return e.store.Get(e.root, searchID)
		}
		return domain.RouteSearchResult{}, err
	}
	releaseAdmission = false
	e.metrics.ActiveRouteSearches.Inc()
	e.appendEvent(result.SearchID, domain.EventSearchAccepted, "Search accepted", nil, &result, nil)
	workerContext, cancel := context.WithTimeout(e.root, time.Duration(request.SearchDeadlineMS)*time.Millisecond)
	e.cancelMu.Lock()
	e.cancels[result.SearchID] = cancel
	e.cancelMu.Unlock()
	e.workerWG.Add(1)
	go func() {
		defer e.workerWG.Done()
		defer func() {
			<-e.admission
			e.metrics.ActiveRouteSearches.Dec()
		}()
		defer cancel()
		defer e.removeCancel(result.SearchID)
		e.run(workerContext, request, result)
	}()
	return result, nil
}

func (e *engine) run(ctx context.Context, request domain.RouteSearchRequest, result domain.RouteSearchResult) {
	started := time.Now()
	capabilities, _ := e.capabilitySnapshot()
	e.stats.searches.Add(1)
	budget := newRequestBudget(request.MaxProviderRequests)
	defer func() {
		if recovered := recover(); recovered != nil {
			slog.Error("route search panic recovered", "search_id", result.SearchID, "error_type", fmt.Sprintf("%T", recovered))
			result.Status = domain.SearchFailed
			result.SelectedRoute = nil
			result.Warnings = appendUniqueString(result.Warnings, "INTERNAL_SEARCH_FAILURE")
			e.finish(ctx, request, result, budget, started, domain.EventSearchFailed)
		}
	}()
	result.Status = domain.SearchSearching
	_ = e.store.Update(ctx, result, e.config.StateTTL)
	e.appendEvent(result.SearchID, domain.EventProviderRequestStarted, "Requesting initial alternatives", nil, nil, nil)

	initialStarted := time.Now()
	response, err := e.routeWithBudget(ctx, budget, contracts.ProviderRouteRequest{
		RequestID: request.RequestID, Origin: request.Origin, Destination: request.Destination,
		Waypoints: request.Waypoints, Traffic: true, Alternatives: minInt(2, maxInt(0, capabilities.MaxAlternatives)),
		AvoidTolls: request.AvoidTolls, AvoidUnpaved: request.AvoidUnpaved, RequestBudget: request.MaxProviderRequests,
		DepartureUnix: departureUnix(request), DeadlineMS: request.SearchDeadlineMS,
	})
	e.metrics.InitialCandidatesDuration.Observe(time.Since(initialStarted).Seconds())
	if err != nil || len(response.Candidates) == 0 {
		e.stats.degraded.Add(1)
		result.Status = domain.SearchDegraded
		result.Warnings = append(result.Warnings, "PROVIDER_INITIAL_CANDIDATES_UNAVAILABLE")
		e.finish(ctx, request, result, budget, started, domain.EventSearchDegraded)
		return
	}
	result.Warnings = append(result.Warnings, providerWarningCodes(response)...)
	valid, invalid := sanitizeProviderCandidates(response.Candidates, e.config.MaxActiveCandidates, "PROVIDER_ALTERNATIVE")
	if invalid > 0 {
		result.Warnings = appendUniqueString(result.Warnings, "INVALID_PROVIDER_CANDIDATES_REJECTED")
	}
	valid, dropped := geometry.Deduplicate(valid, geometry.DefaultDedupeConfig())
	e.metrics.CandidatesGenerated.Add(float64(len(valid)))
	e.metrics.CandidatesDeduplicated.Add(float64(dropped))
	if len(valid) == 0 {
		result.Status = domain.SearchDegraded
		result.Warnings = append(result.Warnings, "NO_VALID_PROVIDER_ROUTES")
		e.finish(ctx, request, result, budget, started, domain.EventSearchDegraded)
		return
	}
	fastest := fastestRoute(valid)
	result.FastestReferenceRoute = cloneCandidate(&fastest)
	e.appendEvent(result.SearchID, domain.EventInitialCandidatesReady, "Initial candidates ready", nil, nil, map[string]interface{}{"count": len(valid), "deduplicated": dropped})

	evaluated := e.evaluateAll(ctx, result.SearchID, request, valid, budget, len(valid) > 1)
	if len(evaluated) == 0 {
		result.Status = domain.SearchDegraded
		result.Warnings = append(result.Warnings, "BASELINE_EVALUATION_UNAVAILABLE")
		e.finish(ctx, request, result, budget, started, domain.EventSearchDegraded)
		return
	}
	fastestEvaluated := fastestRoute(evaluated)
	result.FastestReferenceRoute = cloneCandidate(&fastestEvaluated)
	ranked := scoring.Rank(evaluated, request, e.scoring)
	// Preserve the public JSON contract (`alternatives` is always an array),
	// including when strict constraints reject every candidate.
	result.Alternatives = append([]domain.RouteCandidate{}, ranked.Eligible...)
	result.BestEffortRoutes = cloneCandidates(ranked.BestEffort)
	result.GreenTopRoutes = topGreenRoutes(evaluated, greenTopRouteCount)
	result.SelectedRoute = cloneCandidate(ranked.Selected)
	if len(ranked.Rejected) > 0 {
		result.Warnings = append(result.Warnings, "ROUTES_REJECTED_BY_HARD_CONSTRAINTS")
	}

	if e.shouldEnhance(request, ranked.Selected) {
		e.appendEvent(result.SearchID, domain.EventDetourSearchStarted, "Searching bounded detours", nil, nil, nil)
		enhancedStarted := time.Now()
		result = e.enhance(ctx, request, result, evaluated, budget)
		e.metrics.EnhancedSearchDuration.Observe(time.Since(enhancedStarted).Seconds())
	}
	if result.SelectedRoute == nil {
		result.Status = domain.SearchDegraded
		result.Warnings = append(result.Warnings, "NO_ROUTE_WITHIN_HARD_CONSTRAINTS")
		if request.RoutingMode == domain.RoutingModeStrictGreen {
			result.Warnings = append(result.Warnings, "NO_VERIFIED_STRICT_GREEN_ROUTE")
			e.metrics.NoGreenRoute.Inc()
		}
		e.stats.degraded.Add(1)
	} else {
		result.Status = domain.SearchCompleted
		if result.SelectedRoute.Metrics.RedDurationSeconds == 0 && !hasUnknownTraffic(*result.SelectedRoute) {
			e.metrics.GreenRouteFound.Inc()
		} else {
			e.metrics.NoGreenRoute.Inc()
			result.Warnings = append(result.Warnings, "NO_GREEN_ROUTE_AVAILABLE")
			result.SelectedRoute.ReasonCodes = appendUniqueString(result.SelectedRoute.ReasonCodes, "NO_GREEN_ROUTE_AVAILABLE")
		}
		if result.SelectedRoute.Confidence.Level == domain.ConfidenceLow {
			e.stats.lowConfidence.Add(1)
			e.metrics.LowConfidenceResults.Inc()
			result.Warnings = append(result.Warnings, "LOW_CONFIDENCE_RESULT")
		}
	}
	e.finish(ctx, request, result, budget, started, terminalEvent(result.Status))
}

func (e *engine) evaluateAll(ctx context.Context, searchID string, request domain.RouteSearchRequest, candidates []domain.RouteCandidate, budget *requestBudget, hasAlternatives bool) []domain.RouteCandidate {
	limit := minInt(3, len(candidates))
	semaphore := make(chan struct{}, maxInt(1, limit))
	results := make([]domain.RouteCandidate, len(candidates))
	valid := make([]bool, len(candidates))
	var wg sync.WaitGroup
	for index, candidate := range candidates {
		wg.Add(1)
		go func(index int, candidate domain.RouteCandidate) {
			defer wg.Done()
			defer func() {
				if recovered := recover(); recovered != nil {
					valid[index] = false
					e.metrics.CandidateEvaluationFailures.Inc()
					slog.Error("candidate evaluation panic isolated", "search_id", searchID, "candidate_id", candidate.CandidateID, "error_type", fmt.Sprintf("%T", recovered))
				}
			}()
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-ctx.Done():
				return
			}
			evaluated := e.evaluateCandidate(ctx, request, candidate, budget, hasAlternatives)
			results[index], valid[index] = evaluated, true
			e.appendEvent(searchID, domain.EventCandidateEvaluated, "Candidate evaluated", nil, nil, candidateProgressMetadata(evaluated))
		}(index, candidate)
	}
	wg.Wait()
	compact := make([]domain.RouteCandidate, 0, len(candidates))
	for i, candidate := range results {
		if valid[i] {
			compact = append(compact, candidate)
		}
	}
	return compact
}

func (e *engine) evaluateCandidate(ctx context.Context, request domain.RouteSearchRequest, candidate domain.RouteCandidate, budget *requestBudget, hasAlternatives bool) domain.RouteCandidate {
	if hasCompleteProviderTrafficClasses(candidate) {
		// Per-geometry provider traffic colors are already aligned to the exact
		// provider-approved route. A second traffic-disabled request cannot add a
		// meaningful per-part baseline and would only spend scarce provider quota.
		return scoring.Evaluate(candidate, trafficEvidence(candidate, true, hasAlternatives), e.scoring)
	}
	if hasCompleteBaseline(candidate) {
		return scoring.Evaluate(candidate, trafficEvidence(candidate, true, hasAlternatives), e.scoring)
	}
	capabilities, _ := e.capabilitySnapshot()
	points := controlPoints(candidate.Geometry, maxInt(2, capabilities.MaxWaypoints))
	providerRequest := contracts.ProviderRouteRequest{
		RequestID: request.RequestID, Origin: points[0], Destination: points[len(points)-1], Traffic: false,
		Alternatives: 0, AvoidTolls: request.AvoidTolls, AvoidUnpaved: request.AvoidUnpaved,
		DeadlineMS: maxInt(500, request.SearchDeadlineMS/2),
	}
	if len(points) > 2 {
		providerRequest.Waypoints = points[1 : len(points)-1]
	}
	response, err := e.routeWithBudget(ctx, budget, providerRequest)
	if err != nil || len(response.Candidates) == 0 {
		candidate.Segments = ensureSegments(candidate)
		for index := range candidate.Segments {
			candidate.Segments[index].BaselineDurationSeconds = 0
			candidate.Segments[index].GeometrySimilarity = 0
		}
		candidate.ReasonCodes = appendUniqueString(candidate.ReasonCodes, "BASELINE_UNAVAILABLE")
		evidence := trafficEvidence(candidate, false, hasAlternatives)
		evidence.ProviderErrors = 1
		return scoring.Evaluate(candidate, evidence, e.scoring)
	}
	baseline := response.Candidates[0]
	baselineDuration := baseline.BaselineDurationSeconds
	if baselineDuration <= 0 {
		baselineDuration = baseline.LiveDurationSeconds
	}
	similarity := geometry.Similarity(candidate.Geometry, baseline.Geometry, 3_000)
	candidate.BaselineDurationSeconds = baselineDuration
	candidate.Segments = ensureSegments(candidate)
	remaining := baselineDuration
	for index := range candidate.Segments {
		segment := &candidate.Segments[index]
		segment.GeometrySimilarity = similarity
		segment.Source = "GREENROUTE_LIVE_BASELINE_COMPARISON"
		if index == len(candidate.Segments)-1 {
			segment.BaselineDurationSeconds = max64(0, remaining)
		} else if candidate.DistanceMeters > 0 {
			segment.BaselineDurationSeconds = int64(math.Round(float64(baselineDuration) * float64(segment.DistanceMeters) / float64(candidate.DistanceMeters)))
			remaining -= segment.BaselineDurationSeconds
		}
	}
	candidate.ProviderRequestCount += response.RequestsUsed
	return scoring.Evaluate(candidate, trafficEvidence(candidate, similarity >= e.scoring.MinimumGeometrySimilarity, hasAlternatives), e.scoring)
}

// enhance runs the green-corridor search. Each iteration re-reads the congestion
// evidence of every candidate collected so far, picks the next untried strategy
// from the ladder in greensearch.go, and asks the provider for a route under
// that constraint. The provider never decides what "green" means; it only
// answers a constrained routing question this loop composed.
func (e *engine) enhance(ctx context.Context, request domain.RouteSearchRequest, result domain.RouteSearchResult, candidates []domain.RouteCandidate, budget *requestBudget) domain.RouteSearchResult {
	capabilities, _ := e.capabilitySnapshot()
	best := result.SelectedRoute
	base := best
	if base == nil && result.FastestReferenceRoute != nil {
		base = result.FastestReferenceRoute
	}
	if base == nil {
		return result
	}
	zonesAllowed := e.config.EnableAvoidZones && capabilities.AvoidZones
	anchorCapacity := maxInt(0, capabilities.MaxWaypoints-len(request.Waypoints)-2)
	// Both green modes treat yellow as an obstacle worth routing around; the
	// cluster weighting still deals with red and orange first.
	const minimumSeverity = 1
	tried := make(map[string]bool, 8)
	probes, consecutiveErrors := 0, 0
	for iteration := 0; iteration < e.config.MaxEnhancedIterations; iteration++ {
		_, remaining, exhausted := budget.snapshot()
		if exhausted || remaining == 0 || ctx.Err() != nil || len(candidates) >= e.config.MaxActiveCandidates {
			break
		}
		clusters := congestionClusters(candidates, request, minimumSeverity)
		plan, ok := nextUntriedPlan(
			greenDetourPlans(clusters, request, zonesAllowed, e.config.EnableCorridorAnchors, anchorCapacity),
			tried,
		)
		if !ok {
			break
		}
		tried[plan.label] = true
		probes++
		providerRequest := contracts.ProviderRouteRequest{
			RequestID: request.RequestID, Origin: request.Origin, Destination: request.Destination,
			Waypoints: append([]domain.GeoPoint(nil), request.Waypoints...), Traffic: true, Alternatives: minInt(2, maxInt(0, capabilities.MaxAlternatives)),
			AvoidTolls: request.AvoidTolls, AvoidUnpaved: request.AvoidUnpaved,
			DepartureUnix: departureUnix(request), DeadlineMS: maxInt(500, request.SearchDeadlineMS/2), RequestBudget: remaining,
			AvoidZones: plan.zones,
		}
		providerRequest.Waypoints = append(providerRequest.Waypoints, plan.anchors...)
		e.appendEvent(result.SearchID, domain.EventDetourSearchStarted, "Probing a greener corridor", nil, nil, map[string]interface{}{
			"strategy": plan.label, "avoidZones": len(plan.zones), "anchors": len(plan.anchors),
			"congestionClusters": len(clusters), "probe": probes,
		})
		response, err := e.routeWithBudget(ctx, budget, providerRequest)
		if err != nil {
			// An unroutable exclusion set is an expected outcome of an aggressive
			// plan, not a failure of the search: the ladder simply moves on. Two
			// failures in a row mean something else — a rate limit, an open
			// breaker, an outage — and continuing would only deepen it.
			result.Warnings = appendUniqueString(result.Warnings, "ENHANCED_SEARCH_PROVIDER_ERROR")
			consecutiveErrors++
			if consecutiveErrors >= 2 {
				result.Warnings = appendUniqueString(result.Warnings, "DETOUR_SEARCH_STOPPED_EARLY")
				break
			}
			continue
		}
		consecutiveErrors = 0
		remainingCapacity := maxInt(0, e.config.MaxActiveCandidates-len(candidates))
		incoming, invalid := sanitizeProviderCandidates(response.Candidates, remainingCapacity, "ENHANCED_DETOUR")
		if invalid > 0 {
			result.Warnings = appendUniqueString(result.Warnings, "INVALID_PROVIDER_CANDIDATES_REJECTED")
		}
		connected := incoming[:0]
		for _, candidate := range incoming {
			if connectsSameTrip(candidate, *base, detourEndpointToleranceMeters) {
				connected = append(connected, candidate)
			}
		}
		if len(connected) < len(incoming) {
			result.Warnings = appendUniqueString(result.Warnings, "DETOUR_ENDPOINTS_RELOCATED_BY_PROVIDER")
		}
		incoming = connected
		if len(incoming) == 0 {
			continue
		}
		evaluated := e.evaluateAll(ctx, result.SearchID, request, incoming, budget, len(candidates)+len(incoming) > 1)
		// `:=` here used to declare a second, loop-scoped `candidates`, so the
		// deduplicated pool was discarded at the end of every iteration and the
		// same route could be carried forward once per probe.
		deduplicated, dropped := geometry.Deduplicate(append(candidates, evaluated...), geometry.DefaultDedupeConfig())
		candidates = deduplicated
		e.metrics.CandidatesDeduplicated.Add(float64(dropped))
		ranked := scoring.Rank(candidates, request, e.scoring)
		fastestCombined := fastestRoute(candidates)
		result.FastestReferenceRoute = cloneCandidate(&fastestCombined)
		result.BestEffortRoutes = cloneCandidates(ranked.BestEffort)
		result.GreenTopRoutes = topGreenRoutes(candidates, greenTopRouteCount)
		if ranked.Selected != nil {
			previous := best
			best = ranked.Selected
			result.SelectedRoute = cloneCandidate(ranked.Selected)
			result.Alternatives = append([]domain.RouteCandidate{}, ranked.Eligible...)
			if materiallyBetter(previous, ranked.Selected, e.config.MinimumScoreImprovement) {
				e.appendEvent(result.SearchID, domain.EventBetterRouteFound, "A materially smoother eligible route was found", nil, nil, candidateProgressMetadata(*ranked.Selected))
			}
		}
	}
	result.GreenTopRoutes = topGreenRoutes(candidates, greenTopRouteCount)
	return result
}

func (e *engine) routeWithBudget(ctx context.Context, budget *requestBudget, request contracts.ProviderRouteRequest) (contracts.ProviderRouteResponse, error) {
	lease, ok := budget.lease(2)
	if !ok {
		return contracts.ProviderRouteResponse{}, fmt.Errorf("request budget exhausted")
	}
	request.RequestBudget = lease.allowance
	response, err := e.provider.routes(ctx, request)
	if err != nil {
		// The adapter may have consumed its full retry allowance before returning
		// a sanitized error. Without an authenticated usage envelope on errors,
		// charging the entire lease is the only fail-closed budget behavior.
		lease.finish(lease.allowance)
		e.stats.providerCalls.Add(int64(lease.allowance))
		return response, err
	}
	lease.finish(response.RequestsUsed)
	e.stats.providerCalls.Add(int64(response.RequestsUsed))
	budget.addBillable(response.EstimatedBillableUnits)
	e.stats.billingUnits.Add(int64(response.EstimatedBillableUnits))
	return response, nil
}

func (e *engine) finish(_ context.Context, request domain.RouteSearchRequest, result domain.RouteSearchResult, budget *requestBudget, started time.Time, eventType domain.EventType) {
	used, _, exhausted := budget.snapshot()
	result.ProviderUsage.RequestsUsed = used
	result.ProviderUsage.EstimatedBillableUnits = budget.billableUnits()
	result.ProviderUsage.BudgetExhausted = exhausted
	result.GeneratedAt = time.Now().UTC()
	result.ExpiresAt = result.GeneratedAt.Add(e.config.StateTTL)
	if exhausted {
		result.Warnings = appendUniqueString(result.Warnings, "SEARCH_BUDGET_EXHAUSTED")
		e.stats.budgetExceeded.Add(1)
		e.metrics.SearchBudgetExhausted.Inc()
	}
	result.Warnings = uniqueStrings(result.Warnings)
	updateContext, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	e.finalize(updateContext, result, eventType, terminalMessage(result.Status))
	e.metrics.RouteSearchDuration.Observe(time.Since(started).Seconds())
	e.metrics.RouteSearchTotal.WithLabelValues(string(result.Status), string(request.RoutingMode)).Inc()
	if result.SelectedRoute != nil && result.FastestReferenceRoute != nil {
		e.metrics.SelectedExtraDistance.Observe(float64(max64(0, result.SelectedRoute.DistanceMeters-result.FastestReferenceRoute.DistanceMeters)))
		e.metrics.SelectedRedDuration.Observe(float64(result.SelectedRoute.Metrics.RedDurationSeconds))
	}
}

func (e *engine) cancel(id string) error {
	e.cancelMu.Lock()
	cancel, exists := e.cancels[id]
	if exists {
		cancel()
		delete(e.cancels, id)
	}
	e.cancelMu.Unlock()
	return e.store.Delete(context.Background(), id)
}

func (e *engine) removeCancel(id string) {
	e.cancelMu.Lock()
	delete(e.cancels, id)
	e.cancelMu.Unlock()
}

func (e *engine) shutdown(ctx context.Context) error {
	e.cancelMu.Lock()
	for _, cancel := range e.cancels {
		cancel()
	}
	e.cancelMu.Unlock()
	done := make(chan struct{})
	go func() { e.workerWG.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (e *engine) appendEvent(searchID string, eventType domain.EventType, message string, candidate *domain.RouteCandidate, result *domain.RouteSearchResult, metadata map[string]interface{}) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := e.store.AppendEvent(ctx, domain.SearchEvent{SearchID: searchID, Type: eventType, Timestamp: time.Now().UTC(), Message: message, Candidate: cloneCandidate(candidate), Result: cloneResult(result), Metadata: metadata}, e.config.StateTTL)
	if err != nil && !errors.Is(err, searchstore.ErrNotFound) {
		slog.Warn("failed to append search event", "search_id", searchID, "event", eventType, "error", err)
	}
}

func (e *engine) finalize(ctx context.Context, result domain.RouteSearchResult, eventType domain.EventType, message string) {
	event := domain.SearchEvent{
		SearchID: result.SearchID, Type: eventType, Timestamp: time.Now().UTC(), Message: message,
		Result: cloneResult(&result),
	}
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		err = e.store.Finalize(ctx, result, event, e.config.StateTTL)
		if err == nil || errors.Is(err, searchstore.ErrNotFound) {
			return
		}
		select {
		case <-ctx.Done():
			attempt = 3
		case <-time.After(time.Duration(attempt+1) * 25 * time.Millisecond):
		}
	}
	slog.Error("failed to atomically finalize route search", "search_id", result.SearchID, "status", result.Status, "error", err)
	e.metrics.SearchFinalizationFailures.Inc()
}

func candidateProgressMetadata(candidate domain.RouteCandidate) map[string]interface{} {
	return map[string]interface{}{
		"candidateId":     candidate.CandidateID,
		"confidenceLevel": candidate.Confidence.Level,
		"trafficDataType": candidate.TrafficDataType,
	}
}

func (e *engine) shouldEnhance(request domain.RouteSearchRequest, selected *domain.RouteCandidate) bool {
	if !e.config.EnableEnhancedSearch || !e.config.EnableReranking {
		return false
	}
	if request.RoutingMode != domain.RoutingModeGreenest && request.RoutingMode != domain.RoutingModeStrictGreen {
		return false
	}
	// STRICT_GREEN is fail-closed: the initial ranking deliberately has no
	// selected route when every provider candidate contains non-green or
	// unverified segments. That is precisely when bounded provider-generated
	// detours are useful, so a nil selection must not suppress enhancement.
	if selected == nil {
		return request.RoutingMode == domain.RoutingModeStrictGreen
	}
	// The goal of the green modes is an all-green drive, so any non-green or
	// unproven stretch is a reason to keep searching — not only a red one.
	// Yellow and unknown coverage is exactly what separates a 93% route from
	// the 100% route the user asked for.
	metrics := selected.Metrics
	return metrics.RedDurationSeconds > 0 || metrics.OrangeDurationSeconds > 0 ||
		metrics.YellowDurationSeconds > 0 || metrics.UnknownDurationSeconds > 0 ||
		metrics.RedDistanceMeters > 0 || metrics.OrangeDistanceMeters > 0 ||
		metrics.YellowDistanceMeters > 0 || metrics.UnknownDistanceMeters > 0
}

func hasCompleteBaseline(candidate domain.RouteCandidate) bool {
	if candidate.BaselineDurationSeconds <= 0 || len(candidate.Segments) == 0 {
		return false
	}
	for _, segment := range candidate.Segments {
		if segment.BaselineDurationSeconds <= 0 || segment.GeometrySimilarity <= 0 {
			return false
		}
	}
	return true
}

func hasCompleteProviderTrafficClasses(candidate domain.RouteCandidate) bool {
	if len(candidate.Segments) == 0 {
		return false
	}
	meaningful := 0
	var coveredDistance, coveredDuration int64
	for _, segment := range candidate.Segments {
		if segment.DistanceMeters <= 0 && segment.LiveDurationSeconds <= 0 {
			continue
		}
		meaningful++
		coveredDistance += max64(0, segment.DistanceMeters)
		coveredDuration += max64(0, segment.LiveDurationSeconds)
		if segment.Source != domain.SegmentSourceDGISTrafficColor {
			return false
		}
		switch segment.CongestionClass {
		case domain.CongestionGreen, domain.CongestionYellow, domain.CongestionOrange, domain.CongestionRed, domain.CongestionUnknown:
		default:
			return false
		}
	}
	return meaningful > 0 && coveredDistance == candidate.DistanceMeters && coveredDuration == candidate.LiveDurationSeconds
}

func ensureSegments(candidate domain.RouteCandidate) []domain.RouteSegment {
	if len(candidate.Segments) > 0 {
		return candidate.Segments
	}
	return []domain.RouteSegment{{SegmentID: candidate.CandidateID + "-0", Geometry: candidate.Geometry, DistanceMeters: candidate.DistanceMeters, LiveDurationSeconds: candidate.LiveDurationSeconds, Source: "ROUTE_LEVEL"}}
}

func controlPoints(points []domain.GeoPoint, maximum int) []domain.GeoPoint {
	if len(points) <= 2 {
		return append([]domain.GeoPoint(nil), points...)
	}
	simplified := geometry.Simplify(points, 100)
	if maximum < 2 {
		maximum = 2
	}
	if len(simplified) <= maximum {
		return simplified
	}
	result := make([]domain.GeoPoint, maximum)
	for index := range result {
		sourceIndex := int(math.Round(float64(index) * float64(len(simplified)-1) / float64(maximum-1)))
		result[index] = simplified[sourceIndex]
	}
	return result
}

func fastestRoute(candidates []domain.RouteCandidate) domain.RouteCandidate {
	ordered := append([]domain.RouteCandidate(nil), candidates...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].LiveDurationSeconds != ordered[j].LiveDurationSeconds {
			return ordered[i].LiveDurationSeconds < ordered[j].LiveDurationSeconds
		}
		return ordered[i].CandidateID < ordered[j].CandidateID
	})
	return ordered[0]
}

const (
	greenTopRouteCount = 3
	// Providers snap trip endpoints onto the road network, so a detour may start
	// a short distance from the reference route. Anything beyond this is a
	// different trip rather than a snapped one.
	detourEndpointToleranceMeters = 400
)

// topGreenRoutes answers the question the user actually asks: "show me the
// routes with the most green driving you found, and by how much". It ranks the
// entire evaluated pool by measured green share and is independent of whether a
// route also satisfies the hard constraints of the selected mode, so a search
// that finds nothing eligible still proves what it looked at.
func topGreenRoutes(candidates []domain.RouteCandidate, limit int) []domain.RouteCandidate {
	comparable := make([]domain.RouteCandidate, 0, len(candidates))
	seen := make(map[string]struct{}, len(candidates))
	for index := range candidates {
		candidate := candidates[index]
		if candidate.TrafficDataType == domain.TrafficDataUnknown || candidate.TrafficDataType == domain.TrafficDataBaseline {
			continue
		}
		if len(candidate.Segments) == 0 {
			continue
		}
		// Two probes can rediscover the same provider route. A ranking is a list
		// of distinct options, so the repeat is dropped rather than shown twice.
		if _, duplicate := seen[candidate.CandidateID]; duplicate {
			continue
		}
		seen[candidate.CandidateID] = struct{}{}
		comparable = append(comparable, candidate)
	}
	sort.SliceStable(comparable, func(i, j int) bool {
		leftDuration, leftDistance := greenShare(comparable[i])
		rightDuration, rightDistance := greenShare(comparable[j])
		switch {
		case leftDuration != rightDuration:
			return leftDuration > rightDuration
		case leftDistance != rightDistance:
			return leftDistance > rightDistance
		case comparable[i].Metrics.RedDurationSeconds != comparable[j].Metrics.RedDurationSeconds:
			return comparable[i].Metrics.RedDurationSeconds < comparable[j].Metrics.RedDurationSeconds
		case comparable[i].LiveDurationSeconds != comparable[j].LiveDurationSeconds:
			return comparable[i].LiveDurationSeconds < comparable[j].LiveDurationSeconds
		default:
			return comparable[i].CandidateID < comparable[j].CandidateID
		}
	})
	if len(comparable) > limit {
		comparable = comparable[:limit]
	}
	ranked := make([]domain.RouteCandidate, 0, len(comparable))
	for index := range comparable {
		candidate := *cloneCandidate(&comparable[index])
		candidate.ReasonCodes = appendUniqueString(candidate.ReasonCodes, fmt.Sprintf("GREEN_RANK_%d", index+1))
		ranked = append(ranked, candidate)
	}
	return ranked
}

func materiallyBetter(previous, next *domain.RouteCandidate, minimum float64) bool {
	if next == nil {
		return false
	}
	if previous == nil {
		return true
	}
	// For green modes Score intentionally does not encode the complete
	// lexicographic ranking tuple. Use the congestion dimensions directly for
	// event hysteresis; selection itself is always taken from scoring.Rank.
	previousTuple := []int64{
		previous.Metrics.RedDurationSeconds,
		previous.Metrics.RedDistanceMeters,
		previous.Metrics.OrangeDurationSeconds,
		previous.Metrics.TotalTrafficDelaySeconds,
		previous.Metrics.WorstContinuousCongestionMeters,
		previous.LiveDurationSeconds,
		previous.DistanceMeters,
	}
	nextTuple := []int64{
		next.Metrics.RedDurationSeconds,
		next.Metrics.RedDistanceMeters,
		next.Metrics.OrangeDurationSeconds,
		next.Metrics.TotalTrafficDelaySeconds,
		next.Metrics.WorstContinuousCongestionMeters,
		next.LiveDurationSeconds,
		next.DistanceMeters,
	}
	for index := range previousTuple {
		if nextTuple[index] == previousTuple[index] {
			continue
		}
		if nextTuple[index] > previousTuple[index] {
			return false
		}
		improvement := float64(previousTuple[index]-nextTuple[index]) / float64(max64(1, previousTuple[index]))
		return improvement >= minimum
	}
	return next.CandidateID != previous.CandidateID && next.Score < previous.Score*(1-minimum)
}

const (
	maxCandidateGeometryPoints  = 4_096
	maxCandidateSegments        = 2_000
	maxCandidateSegmentPoints   = 20_000
	maxCandidateDistanceMeters  = 10_000_000
	geometryDistanceSlackMeters = 25_000
)

// sanitizeProviderCandidates treats every provider response as untrusted. It
// applies the same validation and resource caps to initial and enhanced paths,
// so malformed detours cannot poison fastest-route selection or panic control
// point generation.
func sanitizeProviderCandidates(candidates []domain.RouteCandidate, limit int, generatedBy string) ([]domain.RouteCandidate, int) {
	if limit <= 0 {
		return nil, len(candidates)
	}
	valid := make([]domain.RouteCandidate, 0, minInt(limit, len(candidates)))
	for _, candidate := range candidates {
		if len(valid) >= limit {
			break
		}
		if candidate.Blocked || candidate.LiveDurationSeconds <= 0 || candidate.DistanceMeters > maxCandidateDistanceMeters || len(candidate.Geometry) > maxCandidateGeometryPoints || len(candidate.Segments) > maxCandidateSegments || candidate.Validate() != nil || !plausiblePolyline(candidate.Geometry, candidate.DistanceMeters) {
			continue
		}
		segmentsValid := true
		totalSegmentPoints := 0
		for _, segment := range candidate.Segments {
			totalSegmentPoints += len(segment.Geometry)
			if segment.DistanceMeters < 0 || segment.LiveDurationSeconds < 0 || segment.BaselineDurationSeconds < 0 || math.IsNaN(segment.GeometrySimilarity) || math.IsInf(segment.GeometrySimilarity, 0) || segment.GeometrySimilarity < 0 || segment.GeometrySimilarity > 1 || len(segment.Geometry) > maxCandidateGeometryPoints || totalSegmentPoints > maxCandidateSegmentPoints || (len(segment.Geometry) > 1 && !plausiblePolyline(segment.Geometry, max64(1, segment.DistanceMeters))) {
				segmentsValid = false
				break
			}
			for _, point := range segment.Geometry {
				if point.Validate() != nil {
					segmentsValid = false
					break
				}
			}
			if !segmentsValid {
				break
			}
		}
		if !segmentsValid {
			continue
		}
		if generatedBy == "ENHANCED_DETOUR" {
			candidate.GeneratedBy = generatedBy
		} else {
			candidate.GeneratedBy = defaultString(candidate.GeneratedBy, generatedBy)
		}
		valid = append(valid, candidate)
	}
	return valid, len(candidates) - len(valid)
}

func plausiblePolyline(points []domain.GeoPoint, reportedDistance int64) bool {
	length := geometry.PolylineLength(points)
	if math.IsNaN(length) || math.IsInf(length, 0) || length > maxCandidateDistanceMeters {
		return false
	}
	maximum := float64(reportedDistance)*2 + geometryDistanceSlackMeters
	return length <= maximum
}

func trafficEvidence(candidate domain.RouteCandidate, geometryAvailable, hasAlternatives bool) scoring.Evidence {
	typeOfData := candidate.TrafficDataType
	if typeOfData == "" {
		// Provider-neutral synthetic/manual candidates created before the evidence
		// field existed remain valid, but production adapters always set it.
		typeOfData = domain.TrafficDataSynthetic
	}
	available := geometryAvailable && typeOfData != domain.TrafficDataUnknown && typeOfData != domain.TrafficDataBaseline
	return scoring.Evidence{TrafficDataAvailable: available, TrafficDataType: typeOfData, HasAlternatives: hasAlternatives}
}

func hasUnknownTraffic(candidate domain.RouteCandidate) bool {
	if candidate.TrafficDataType == domain.TrafficDataUnknown || candidate.TrafficDataType == domain.TrafficDataBaseline {
		return true
	}
	for _, segment := range candidate.Segments {
		if segment.CongestionClass == domain.CongestionUnknown {
			return true
		}
	}
	return candidate.Metrics.UnknownDurationSeconds > 0 || candidate.Metrics.UnknownDistanceMeters > 0
}

func terminalEvent(status domain.SearchStatus) domain.EventType {
	if status == domain.SearchCompleted {
		return domain.EventSearchCompleted
	}
	if status == domain.SearchDegraded {
		return domain.EventSearchDegraded
	}
	return domain.EventSearchFailed
}

func terminalMessage(status domain.SearchStatus) string {
	switch status {
	case domain.SearchCompleted:
		return "Search completed"
	case domain.SearchDegraded:
		return "Search completed with degraded data"
	case domain.SearchCancelled:
		return "Search cancelled"
	default:
		return "Search failed"
	}
}

func cloneCandidate(value *domain.RouteCandidate) *domain.RouteCandidate {
	if value == nil {
		return nil
	}
	copy := *value
	copy.Geometry = append([]domain.GeoPoint(nil), value.Geometry...)
	copy.Confidence.Reasons = append([]string(nil), value.Confidence.Reasons...)
	copy.Segments = make([]domain.RouteSegment, len(value.Segments))
	for index, segment := range value.Segments {
		copy.Segments[index] = segment
		copy.Segments[index].Geometry = append([]domain.GeoPoint(nil), segment.Geometry...)
		copy.Segments[index].Confidence.Reasons = append([]string(nil), segment.Confidence.Reasons...)
	}
	copy.ReasonCodes = append([]string(nil), value.ReasonCodes...)
	return &copy
}

func cloneCandidates(values []domain.RouteCandidate) []domain.RouteCandidate {
	result := make([]domain.RouteCandidate, 0, len(values))
	for index := range values {
		result = append(result, *cloneCandidate(&values[index]))
	}
	return result
}

func cloneResult(value *domain.RouteSearchResult) *domain.RouteSearchResult {
	if value == nil {
		return nil
	}
	copy := *value
	copy.SelectedRoute = cloneCandidate(value.SelectedRoute)
	copy.FastestReferenceRoute = cloneCandidate(value.FastestReferenceRoute)
	// A nil destination would turn a valid empty slice back into JSON null in
	// SSE snapshots. Clone onto non-nil slices to retain the API invariant.
	copy.Alternatives = cloneCandidates(value.Alternatives)
	copy.BestEffortRoutes = cloneCandidates(value.BestEffortRoutes)
	copy.GreenTopRoutes = cloneCandidates(value.GreenTopRoutes)
	copy.Warnings = append([]string{}, value.Warnings...)
	return &copy
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func appendUniqueString(values []string, value string) []string {
	return uniqueStrings(append(values, value))
}

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func departureUnix(request domain.RouteSearchRequest) int64 {
	if request.DepartureTime == nil {
		return 0
	}
	return request.DepartureTime.Unix()
}

func providerWarningCodes(response contracts.ProviderRouteResponse) []string {
	result := make([]string, 0, len(response.Warnings)+1)
	if response.Degraded {
		result = append(result, "PROVIDER_DEGRADED")
	}
	for _, warning := range response.Warnings {
		normalized := strings.ToLower(warning)
		switch {
		case strings.Contains(normalized, "synthetic"):
			result = appendUniqueString(result, "SYNTHETIC_PROVIDER_DATA")
		case strings.Contains(normalized, "billable") || strings.Contains(normalized, "commercial contract"):
			result = appendUniqueString(result, "PROVIDER_BILLING_UNITS_ESTIMATED")
		case strings.Contains(normalized, "congestion") || strings.Contains(normalized, "traffic"):
			result = appendUniqueString(result, "PROVIDER_SEGMENT_CONGESTION_UNAVAILABLE")
		default:
			result = appendUniqueString(result, "PROVIDER_WARNING")
		}
	}
	return result
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
