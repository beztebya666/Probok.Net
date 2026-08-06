package searchstore

import (
	"context"
	"testing"
	"time"

	"github.com/greenroute/greenroute/internal/domain"
)

func TestMemoryFinalizePublishesTerminalEventAndSnapshotAtomically(t *testing.T) {
	store := NewMemory()
	ctx := context.Background()
	initial := domain.RouteSearchResult{SearchID: "search-finalize", Status: domain.SearchSearching}
	if err := store.Create(ctx, initial, time.Minute); err != nil {
		t.Fatal(err)
	}
	final := initial
	final.Status = domain.SearchCompleted
	event := domain.SearchEvent{SearchID: initial.SearchID, Type: domain.EventSearchCompleted, Result: &final}
	if err := store.Finalize(ctx, final, event, time.Minute); err != nil {
		t.Fatal(err)
	}

	got, err := store.Get(ctx, initial.SearchID)
	if err != nil || got.Status != domain.SearchCompleted {
		t.Fatalf("terminal snapshot missing: result=%#v err=%v", got, err)
	}
	events, err := store.EventsAfter(ctx, initial.SearchID, 0)
	if err != nil || len(events) != 1 || events[0].Type != domain.EventSearchCompleted || events[0].EventID != 1 {
		t.Fatalf("terminal event missing: events=%#v err=%v", events, err)
	}

	// A transport-level retry after an ambiguous response must be idempotent.
	if err := store.Finalize(ctx, final, event, time.Minute); err != nil {
		t.Fatal(err)
	}
	events, _ = store.EventsAfter(ctx, initial.SearchID, 0)
	if len(events) != 1 {
		t.Fatalf("finalization retry duplicated terminal event: %#v", events)
	}
}

func TestMemoryFinalizeCannotResurrectDeletedSearch(t *testing.T) {
	store := NewMemory()
	ctx := context.Background()
	initial := domain.RouteSearchResult{SearchID: "search-deleted", Status: domain.SearchSearching}
	if err := store.Create(ctx, initial, time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(ctx, initial.SearchID); err != nil {
		t.Fatal(err)
	}
	initial.Status = domain.SearchCompleted
	err := store.Finalize(ctx, initial, domain.SearchEvent{SearchID: initial.SearchID, Type: domain.EventSearchCompleted}, time.Minute)
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if _, err := store.Get(ctx, initial.SearchID); err != ErrNotFound {
		t.Fatalf("deleted search was resurrected: %v", err)
	}
}

func TestMemoryEventsAfterUsesCursorAndBoundsReplayBatch(t *testing.T) {
	store := NewMemory()
	ctx := context.Background()
	initial := domain.RouteSearchResult{SearchID: "search-replay", Status: domain.SearchSearching}
	if err := store.Create(ctx, initial, time.Minute); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < maximumEventReplayBatch+10; index++ {
		if _, err := store.AppendEvent(ctx, domain.SearchEvent{SearchID: initial.SearchID, Type: domain.EventCandidateEvaluated}, time.Minute); err != nil {
			t.Fatal(err)
		}
	}

	first, err := store.EventsAfter(ctx, initial.SearchID, 0)
	if err != nil || len(first) != maximumEventReplayBatch || first[0].EventID != 1 || first[len(first)-1].EventID != maximumEventReplayBatch {
		t.Fatalf("unexpected first replay batch: len=%d err=%v", len(first), err)
	}
	second, err := store.EventsAfter(ctx, initial.SearchID, first[len(first)-1].EventID)
	if err != nil || len(second) != 10 || second[0].EventID != maximumEventReplayBatch+1 {
		t.Fatalf("unexpected second replay batch: events=%#v err=%v", second, err)
	}
}

func TestMemoryActiveBeforeExcludesAtomicallyFinalizedSearches(t *testing.T) {
	store := NewMemory()
	ctx := context.Background()
	initial := domain.RouteSearchResult{
		SearchID: "search-active-index", Status: domain.SearchSearching,
		GeneratedAt: time.Now().Add(-2 * time.Minute),
	}
	if err := store.Create(ctx, initial, time.Minute); err != nil {
		t.Fatal(err)
	}
	active, err := store.ActiveBefore(ctx, time.Now().Add(-time.Minute), 10)
	if err != nil || len(active) != 1 || active[0].SearchID != initial.SearchID {
		t.Fatalf("active search not discoverable: %#v err=%v", active, err)
	}
	final := initial
	final.Status = domain.SearchFailed
	if err := store.Finalize(ctx, final, domain.SearchEvent{SearchID: initial.SearchID, Type: domain.EventSearchFailed}, time.Minute); err != nil {
		t.Fatal(err)
	}
	active, err = store.ActiveBefore(ctx, time.Now(), 10)
	if err != nil || len(active) != 0 {
		t.Fatalf("terminal search remained active: %#v err=%v", active, err)
	}
}

func TestMemoryStorePreservesAndIsolatesBestEffortRoutes(t *testing.T) {
	store := NewMemory()
	ctx := context.Background()
	result := domain.RouteSearchResult{
		SearchID: "search-best-effort", Status: domain.SearchDegraded,
		Alternatives: []domain.RouteCandidate{},
		BestEffortRoutes: []domain.RouteCandidate{{
			CandidateID: "near-green", ReasonCodes: []string{"BEST_EFFORT_NOT_STRICT_GREEN", "STRICT_GREEN_NON_GREEN_SEGMENT"},
			Geometry: []domain.GeoPoint{{Latitude: 55.7, Longitude: 37.5}, {Latitude: 55.8, Longitude: 37.6}},
		}},
	}
	if err := store.Create(ctx, result, time.Minute); err != nil {
		t.Fatal(err)
	}
	first, err := store.Get(ctx, result.SearchID)
	if err != nil || len(first.BestEffortRoutes) != 1 {
		t.Fatalf("best effort route missing from store: %#v err=%v", first, err)
	}
	first.BestEffortRoutes[0].ReasonCodes[0] = "MUTATED"
	first.BestEffortRoutes[0].Geometry[0].Latitude = 0
	second, err := store.Get(ctx, result.SearchID)
	if err != nil {
		t.Fatal(err)
	}
	if second.BestEffortRoutes[0].ReasonCodes[0] != "BEST_EFFORT_NOT_STRICT_GREEN" || second.BestEffortRoutes[0].Geometry[0].Latitude != 55.7 {
		t.Fatalf("stored best-effort route was aliased: %#v", second.BestEffortRoutes[0])
	}
}
