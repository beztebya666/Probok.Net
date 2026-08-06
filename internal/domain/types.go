package domain

import "time"

type RoutingMode string

const (
	RoutingModeFastest     RoutingMode = "FASTEST"
	RoutingModeBalanced    RoutingMode = "BALANCED"
	RoutingModeGreenest    RoutingMode = "GREENEST"
	RoutingModeStrictGreen RoutingMode = "STRICT_GREEN"
)

func (m RoutingMode) Valid() bool {
	switch m {
	case RoutingModeFastest, RoutingModeBalanced, RoutingModeGreenest, RoutingModeStrictGreen:
		return true
	default:
		return false
	}
}

type CongestionClass string

const (
	CongestionGreen   CongestionClass = "GREEN"
	CongestionYellow  CongestionClass = "YELLOW"
	CongestionOrange  CongestionClass = "ORANGE"
	CongestionRed     CongestionClass = "RED"
	CongestionUnknown CongestionClass = "UNKNOWN"
)

type ConfidenceLevel string

const (
	ConfidenceHigh   ConfidenceLevel = "HIGH"
	ConfidenceMedium ConfidenceLevel = "MEDIUM"
	ConfidenceLow    ConfidenceLevel = "LOW"
)

type SearchStatus string

const (
	SearchAccepted  SearchStatus = "ACCEPTED"
	SearchSearching SearchStatus = "SEARCHING"
	SearchCompleted SearchStatus = "COMPLETED"
	SearchDegraded  SearchStatus = "DEGRADED"
	SearchFailed    SearchStatus = "FAILED"
	SearchCancelled SearchStatus = "CANCELLED"
)

type EventType string

const (
	EventSearchAccepted         EventType = "SEARCH_ACCEPTED"
	EventProviderRequestStarted EventType = "PROVIDER_REQUEST_STARTED"
	EventInitialCandidatesReady EventType = "INITIAL_CANDIDATES_READY"
	EventCandidateEvaluated     EventType = "CANDIDATE_EVALUATED"
	EventDetourSearchStarted    EventType = "DETOUR_SEARCH_STARTED"
	EventBetterRouteFound       EventType = "BETTER_ROUTE_FOUND"
	EventSearchCompleted        EventType = "SEARCH_COMPLETED"
	EventSearchDegraded         EventType = "SEARCH_DEGRADED"
	EventSearchFailed           EventType = "SEARCH_FAILED"
)

type GeoPoint struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

type Confidence struct {
	Level   ConfidenceLevel `json:"level"`
	Score   float64         `json:"score"`
	Reasons []string        `json:"reasons,omitempty"`
}

type RouteSegment struct {
	SegmentID               string          `json:"segmentId"`
	Geometry                []GeoPoint      `json:"geometry"`
	DistanceMeters          int64           `json:"distanceMeters"`
	LiveDurationSeconds     int64           `json:"liveDurationSeconds"`
	BaselineDurationSeconds int64           `json:"baselineDurationSeconds"`
	TrafficRatio            float64         `json:"trafficRatio"`
	CongestionClass         CongestionClass `json:"congestionClass"`
	Confidence              Confidence      `json:"confidence"`
	GeometrySimilarity      float64         `json:"geometrySimilarity"`
	Source                  string          `json:"source"`
}

const (
	// SegmentSourceDGISTrafficColor marks a congestion class inferred from the
	// per-geometry traffic color returned by the official 2GIS Routing API.
	// The class is provider evidence rather than a live/baseline ratio and must
	// therefore be preserved by scoring instead of reclassified.
	SegmentSourceDGISTrafficColor = "DGIS_ROUTING_API_TRAFFIC_COLOR"
)

type RouteMetrics struct {
	GreenDistanceMeters             int64   `json:"greenDistanceMeters"`
	YellowDistanceMeters            int64   `json:"yellowDistanceMeters"`
	OrangeDistanceMeters            int64   `json:"orangeDistanceMeters"`
	RedDistanceMeters               int64   `json:"redDistanceMeters"`
	UnknownDistanceMeters           int64   `json:"unknownDistanceMeters"`
	GreenDurationSeconds            int64   `json:"greenDurationSeconds"`
	YellowDurationSeconds           int64   `json:"yellowDurationSeconds"`
	OrangeDurationSeconds           int64   `json:"orangeDurationSeconds"`
	RedDurationSeconds              int64   `json:"redDurationSeconds"`
	UnknownDurationSeconds          int64   `json:"unknownDurationSeconds"`
	TotalTrafficDelaySeconds        int64   `json:"totalTrafficDelaySeconds"`
	CongestedDistancePercent        float64 `json:"congestedDistancePercent"`
	CongestedDurationPercent        float64 `json:"congestedDurationPercent"`
	GreenDistancePercent            float64 `json:"greenDistancePercent"`
	WorstContinuousCongestionMeters int64   `json:"worstContinuousCongestionMeters"`
}

type TrafficDataType string

const (
	TrafficDataRealtime  TrafficDataType = "REALTIME"
	TrafficDataForecast  TrafficDataType = "FORECAST"
	TrafficDataBaseline  TrafficDataType = "BASELINE"
	TrafficDataSynthetic TrafficDataType = "SYNTHETIC"
	TrafficDataUnknown   TrafficDataType = "UNKNOWN"
)

type RouteCandidate struct {
	CandidateID             string          `json:"candidateId"`
	Provider                string          `json:"provider"`
	ProviderRouteReference  string          `json:"providerRouteReference,omitempty"`
	TrafficDataType         TrafficDataType `json:"trafficDataType"`
	Geometry                []GeoPoint      `json:"geometry"`
	DistanceMeters          int64           `json:"distanceMeters"`
	LiveDurationSeconds     int64           `json:"liveDurationSeconds"`
	BaselineDurationSeconds int64           `json:"baselineDurationSeconds"`
	TrafficDelaySeconds     int64           `json:"trafficDelaySeconds"`
	Segments                []RouteSegment  `json:"segments"`
	Blocked                 bool            `json:"blocked"`
	Tolls                   bool            `json:"tolls"`
	Unpaved                 bool            `json:"unpaved"`
	Confidence              Confidence      `json:"confidence"`
	Score                   float64         `json:"score"`
	ReasonCodes             []string        `json:"reasonCodes"`
	Explanation             string          `json:"explanation"`
	GeneratedBy             string          `json:"generatedBy"`
	ProviderRequestCount    int             `json:"providerRequestCount"`
	Metrics                 RouteMetrics    `json:"metrics"`
}

type RouteSearchRequest struct {
	RequestID               string      `json:"requestId"`
	Origin                  GeoPoint    `json:"origin"`
	Destination             GeoPoint    `json:"destination"`
	Waypoints               []GeoPoint  `json:"waypoints,omitempty"`
	DepartureTime           *time.Time  `json:"departureTime,omitempty"`
	RoutingMode             RoutingMode `json:"routingMode"`
	MaxExtraDistanceMeters  int64       `json:"maxExtraDistanceMeters"`
	MaxExtraDistancePercent float64     `json:"maxExtraDistancePercent"`
	MaxExtraTimeSeconds     int64       `json:"maxExtraTimeSeconds"`
	AvoidTolls              bool        `json:"avoidTolls"`
	AvoidUnpaved            bool        `json:"avoidUnpaved"`
	Strictness              float64     `json:"strictness"`
	MaxProviderRequests     int         `json:"maxProviderRequests"`
	SearchDeadlineMS        int         `json:"searchDeadlineMs"`
}

type AppliedConstraints struct {
	MaxExtraDistanceMeters  int64   `json:"maxExtraDistanceMeters"`
	MaxExtraDistancePercent float64 `json:"maxExtraDistancePercent"`
	MaxExtraTimeSeconds     int64   `json:"maxExtraTimeSeconds"`
	MinimumConfidenceScore  float64 `json:"minimumConfidenceScore"`
	AvoidTolls              bool    `json:"avoidTolls"`
	AvoidUnpaved            bool    `json:"avoidUnpaved"`
}

type ProviderUsage struct {
	Provider               string  `json:"provider"`
	RequestsUsed           int     `json:"requestsUsed"`
	RequestBudget          int     `json:"requestBudget"`
	EstimatedBillableUnits int     `json:"estimatedBillableUnits"`
	EstimatedCost          float64 `json:"estimatedCost"`
	BudgetExhausted        bool    `json:"budgetExhausted"`
}

type RouteSearchResult struct {
	SearchID         string           `json:"searchId"`
	RequestID        string           `json:"requestId"`
	Status           SearchStatus     `json:"status"`
	SelectedRoute    *RouteCandidate  `json:"selectedRoute,omitempty"`
	Alternatives     []RouteCandidate `json:"alternatives"`
	BestEffortRoutes []RouteCandidate `json:"bestEffortRoutes"`
	// GreenTopRoutes is the proof-of-search ranking: the routes with the highest
	// measured green share found during this search, in every routing mode and
	// regardless of whether they satisfy the mode's hard constraints.
	GreenTopRoutes        []RouteCandidate   `json:"greenTopRoutes"`
	FastestReferenceRoute *RouteCandidate    `json:"fastestReferenceRoute,omitempty"`
	Constraints           AppliedConstraints `json:"constraints"`
	ProviderUsage         ProviderUsage      `json:"providerUsage"`
	Warnings              []string           `json:"warnings"`
	ScoringPolicyVersion  string             `json:"scoringPolicyVersion"`
	GeneratedAt           time.Time          `json:"generatedAt"`
	ExpiresAt             time.Time          `json:"expiresAt"`
}

type SearchEvent struct {
	EventID   int64                  `json:"eventId"`
	SearchID  string                 `json:"searchId"`
	Type      EventType              `json:"type"`
	Timestamp time.Time              `json:"timestamp"`
	Message   string                 `json:"message,omitempty"`
	Candidate *RouteCandidate        `json:"candidate,omitempty"`
	Result    *RouteSearchResult     `json:"result,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

type GeoSuggestion struct {
	ID       string   `json:"id"`
	Label    string   `json:"label"`
	Subtitle string   `json:"subtitle,omitempty"`
	Point    GeoPoint `json:"point"`
}
