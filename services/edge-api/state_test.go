package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMemoryIdempotencyReplayAndConflict(t *testing.T) {
	state := newMemoryState()
	ctx := context.Background()
	existing, claimToken, err := state.BeginIdempotency(ctx, "key", "hash-a", time.Minute, 30*time.Second)
	if err != nil || existing != nil || claimToken == "" {
		t.Fatalf("first begin: existing=%v claim=%q err=%v", existing, claimToken, err)
	}
	if _, _, err := state.BeginIdempotency(ctx, "key", "hash-a", time.Minute, 30*time.Second); !errors.Is(err, errStateInProgress) {
		t.Fatalf("expected in progress, got %v", err)
	}
	if err := state.CompleteIdempotency(ctx, "key", "hash-a", claimToken, "search-id", time.Minute); err != nil {
		t.Fatal(err)
	}
	replay, _, err := state.BeginIdempotency(ctx, "key", "hash-a", time.Minute, 30*time.Second)
	if err != nil || replay == nil || replay.SearchID != "search-id" {
		t.Fatalf("replay=%v err=%v", replay, err)
	}
	if _, _, err := state.BeginIdempotency(ctx, "key", "hash-b", time.Minute, 30*time.Second); !errors.Is(err, errStateConflict) {
		t.Fatalf("expected conflict, got %v", err)
	}
}

func TestMemoryIdempotencyStaleClaimCannotOverwriteOrDeleteReclaimedKey(t *testing.T) {
	state := newMemoryState()
	ctx := context.Background()
	_, staleClaim, err := state.BeginIdempotency(ctx, "key", "hash", time.Minute, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	state.mu.Lock()
	stored := state.idempotency["key"]
	stored.expiresAt = time.Now().Add(-time.Second)
	state.idempotency["key"] = stored
	state.mu.Unlock()

	if _, _, err := state.BeginIdempotency(ctx, "key", "different-hash", time.Minute, 30*time.Second); !errors.Is(err, errStateConflict) {
		t.Fatalf("fingerprint must outlive the short claim, got %v", err)
	}
	_, currentClaim, err := state.BeginIdempotency(ctx, "key", "hash", time.Minute, 30*time.Second)
	if err != nil || currentClaim == "" || currentClaim == staleClaim {
		t.Fatalf("reclaim: claim=%q stale=%q err=%v", currentClaim, staleClaim, err)
	}
	if err := state.CompleteIdempotency(ctx, "key", "hash", staleClaim, "stale-search", time.Minute); !errors.Is(err, errStateConflict) {
		t.Fatalf("stale completion must fail, got %v", err)
	}
	if err := state.ForgetIdempotency(ctx, "key", staleClaim); !errors.Is(err, errStateConflict) {
		t.Fatalf("stale forget must fail, got %v", err)
	}
	if err := state.CompleteIdempotency(ctx, "key", "hash", currentClaim, "current-search", time.Minute); err != nil {
		t.Fatal(err)
	}
	replay, _, err := state.BeginIdempotency(ctx, "key", "hash", time.Minute, 30*time.Second)
	if err != nil || replay == nil || replay.SearchID != "current-search" {
		t.Fatalf("replay=%v err=%v", replay, err)
	}
}

func TestMemoryIdempotencyRecordRepairsMissingFingerprintWithoutPoisoning(t *testing.T) {
	state := newMemoryState()
	ctx := context.Background()
	_, _, err := state.BeginIdempotency(ctx, "key", "original-hash", time.Minute, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	state.mu.Lock()
	delete(state.idempotencyFingerprints, "key")
	state.mu.Unlock()

	if _, _, err := state.BeginIdempotency(ctx, "key", "different-hash", time.Minute, 30*time.Second); !errors.Is(err, errStateConflict) {
		t.Fatalf("authoritative record must reject a conflicting fingerprint repair, got %v", err)
	}
	if _, _, err := state.BeginIdempotency(ctx, "key", "original-hash", time.Minute, 30*time.Second); !errors.Is(err, errStateInProgress) {
		t.Fatalf("original request must remain recoverable, got %v", err)
	}
}

func TestOwnershipIsSubjectScoped(t *testing.T) {
	state := newMemoryState()
	ctx := context.Background()
	if err := state.SetOwner(ctx, "search", "alice", time.Minute); err != nil {
		t.Fatal(err)
	}
	if owns, _ := state.Owns(ctx, "search", "alice"); !owns {
		t.Fatal("owner lost access")
	}
	if owns, _ := state.Owns(ctx, "search", "bob"); owns {
		t.Fatal("different subject gained access")
	}
}
