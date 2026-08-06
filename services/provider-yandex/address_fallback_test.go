package main

import (
	"context"
	"errors"
	"testing"

	"github.com/greenroute/greenroute/internal/contracts"
	"github.com/greenroute/greenroute/internal/domain"
)

type fakeAddressResolver struct {
	result contracts.GeosuggestResponse
	err    error
	ready  error
	calls  int
}

func (r *fakeAddressResolver) Suggest(context.Context, string, string, int) (contracts.GeosuggestResponse, error) {
	r.calls++
	return r.result, r.err
}

func (r *fakeAddressResolver) Ready() error { return r.ready }

func TestFallbackAddressResolverUsesFallbackOnlyAfterPrimaryError(t *testing.T) {
	primaryErr := errors.New("primary unavailable")
	primary := &fakeAddressResolver{err: primaryErr}
	fallbackResult := contracts.GeosuggestResponse{}
	fallback := &fakeAddressResolver{result: fallbackResult}
	metrics := &serviceMetrics{}
	resolver := &fallbackAddressResolver{primary: primary, fallback: fallback, metrics: metrics}

	result, err := resolver.Suggest(context.Background(), "Moscow", "en_RU", 5)
	if err != nil {
		t.Fatal(err)
	}
	if primary.calls != 1 || fallback.calls != 1 {
		t.Fatalf("primary calls=%d fallback calls=%d, want 1 and 1", primary.calls, fallback.calls)
	}
	if metrics.geocoderFallbackError.Load() != 1 || metrics.geocoderFallbackEmpty.Load() != 0 {
		t.Fatalf("fallback metrics error=%d empty=%d", metrics.geocoderFallbackError.Load(), metrics.geocoderFallbackEmpty.Load())
	}
	if result.Suggestions != nil {
		t.Fatalf("unexpected result: %#v", result)
	}

	primary.err = nil
	primary.result = contracts.GeosuggestResponse{Suggestions: []domain.GeoSuggestion{{ID: "primary-result"}}}
	if _, err := resolver.Suggest(context.Background(), "Moscow", "en_RU", 5); err != nil {
		t.Fatal(err)
	}
	if primary.calls != 2 || fallback.calls != 1 {
		t.Fatalf("successful primary must not call fallback: primary=%d fallback=%d", primary.calls, fallback.calls)
	}
}

func TestFallbackAddressResolverChecksFallbackAfterEmptyPrimaryResult(t *testing.T) {
	primary := &fakeAddressResolver{result: contracts.GeosuggestResponse{Suggestions: []domain.GeoSuggestion{}}}
	fallbackResult := contracts.GeosuggestResponse{Suggestions: []domain.GeoSuggestion{{ID: "fallback-result"}}}
	fallback := &fakeAddressResolver{result: fallbackResult}
	metrics := &serviceMetrics{}
	resolver := &fallbackAddressResolver{primary: primary, fallback: fallback, metrics: metrics}

	result, err := resolver.Suggest(context.Background(), "Moscow", "en_RU", 5)
	if err != nil {
		t.Fatal(err)
	}
	if primary.calls != 1 || fallback.calls != 1 || len(result.Suggestions) != 1 || result.Suggestions[0].ID != "fallback-result" {
		t.Fatalf("primary=%d fallback=%d result=%#v", primary.calls, fallback.calls, result)
	}
	if metrics.geocoderFallbackEmpty.Load() != 1 || metrics.geocoderFallbackError.Load() != 0 {
		t.Fatalf("fallback metrics empty=%d error=%d", metrics.geocoderFallbackEmpty.Load(), metrics.geocoderFallbackError.Load())
	}
}

func TestFallbackAddressResolverSkipsPrimaryEgressWhenPrimaryIsNotReady(t *testing.T) {
	primary := &fakeAddressResolver{ready: errors.New("credential fault")}
	fallback := &fakeAddressResolver{result: contracts.GeosuggestResponse{Suggestions: []domain.GeoSuggestion{{ID: "fallback-result"}}}}
	resolver := &fallbackAddressResolver{primary: primary, fallback: fallback}

	result, err := resolver.Suggest(context.Background(), "Moscow", "en_RU", 5)
	if err != nil {
		t.Fatal(err)
	}
	if primary.calls != 0 || fallback.calls != 1 || len(result.Suggestions) != 1 {
		t.Fatalf("primary=%d fallback=%d result=%#v", primary.calls, fallback.calls, result)
	}
}

func TestFallbackAddressResolverPreservesEmptyPrimaryWhenFallbackFails(t *testing.T) {
	primary := &fakeAddressResolver{result: contracts.GeosuggestResponse{Suggestions: []domain.GeoSuggestion{}}}
	fallback := &fakeAddressResolver{err: errors.New("fallback unavailable")}
	resolver := &fallbackAddressResolver{primary: primary, fallback: fallback}

	result, err := resolver.Suggest(context.Background(), "Moscow", "en_RU", 5)
	if err != nil || result.Suggestions == nil || len(result.Suggestions) != 0 {
		t.Fatalf("result=%#v err=%v, want empty successful primary result", result, err)
	}
}

func TestFallbackAddressResolverHonorsCallerCancellation(t *testing.T) {
	primary := &fakeAddressResolver{err: context.Canceled}
	fallback := &fakeAddressResolver{}
	resolver := &fallbackAddressResolver{primary: primary, fallback: fallback}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := resolver.Suggest(ctx, "Moscow", "en_RU", 5)
	if !errors.Is(err, context.Canceled) || fallback.calls != 0 {
		t.Fatalf("err=%v fallback calls=%d, want cancellation without fallback", err, fallback.calls)
	}
}

func TestFallbackAddressResolverReadyWhenEitherResolverIsReady(t *testing.T) {
	unavailable := errors.New("unavailable")
	tests := []struct {
		name          string
		primaryReady  error
		fallbackReady error
		wantError     bool
	}{
		{name: "primary ready", fallbackReady: unavailable},
		{name: "fallback ready", primaryReady: unavailable},
		{name: "both unavailable", primaryReady: unavailable, fallbackReady: unavailable, wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := &fallbackAddressResolver{
				primary:  &fakeAddressResolver{ready: tt.primaryReady},
				fallback: &fakeAddressResolver{ready: tt.fallbackReady},
			}
			err := resolver.Ready()
			if (err != nil) != tt.wantError {
				t.Fatalf("Ready() error=%v, wantError=%v", err, tt.wantError)
			}
		})
	}
}
