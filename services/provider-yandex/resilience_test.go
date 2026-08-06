package main

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

func TestSharedCooldownBlocksConcurrentOutboundAttempts(t *testing.T) {
	fixture := string(readContractFixture(t))
	var calls atomic.Int32
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
		call := calls.Add(1)
		if call == 1 {
			result := response(http.StatusTooManyRequests, `{}`)
			result.Header.Set("Retry-After", "10")
			return result, nil
		}
		return response(http.StatusOK, fixture), nil
	})
	cfg := testConfig()
	cfg.MaxRetries = 1
	cfg.MaxConcurrency = 8
	adapter := newYandexAdapter(cfg, &http.Client{Transport: transport}, &serviceMetrics{})
	adapter.now = func() time.Time { return time.Unix(1_700_000_000, 0) }
	adapter.cooldownJitter = func(time.Duration) time.Duration { return 0 }

	const concurrentFollowers = 3
	waits := make(chan time.Duration, concurrentFollowers+1)
	release := make(chan struct{})
	adapter.sleep = func(ctx context.Context, delay time.Duration) error {
		waits <- delay
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	results := make(chan error, concurrentFollowers+1)
	go func() {
		_, err := adapter.Routes(context.Background(), validProviderRequest())
		results <- err
	}()
	if delay := receiveDuration(t, waits); delay != 10*time.Second {
		t.Fatalf("first cooldown wait=%s, want 10s", delay)
	}

	for range concurrentFollowers {
		go func() {
			_, err := adapter.Routes(context.Background(), validProviderRequest())
			results <- err
		}()
	}
	for range concurrentFollowers {
		if delay := receiveDuration(t, waits); delay != 10*time.Second {
			t.Fatalf("follower cooldown wait=%s, want 10s", delay)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("outbound attempts during shared cooldown=%d, want only the original 429", got)
	}

	close(release)
	for range concurrentFollowers + 1 {
		select {
		case err := <-results:
			if err != nil {
				t.Fatalf("request after cooldown failed: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for requests after cooldown")
		}
	}
	if got, want := calls.Load(), int32(concurrentFollowers+2); got != want {
		t.Fatalf("total outbound attempts=%d, want %d", got, want)
	}
}

func TestRouter429CooldownAlsoGatesGeocoderWithBoundedJitter(t *testing.T) {
	var geocoderBeforeWait atomic.Bool
	var waited atomic.Bool
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Hostname() {
		case "api.routing.yandex.net":
			result := response(http.StatusTooManyRequests, `{}`)
			result.Header.Set("Retry-After", "2")
			return result, nil
		case "geocode-maps.yandex.ru":
			if !waited.Load() {
				geocoderBeforeWait.Store(true)
			}
			return response(http.StatusOK, `{}`), nil
		default:
			return response(http.StatusBadGateway, `{}`), nil
		}
	})
	cfg := testConfig()
	cfg.MaxRetries = 0
	adapter := newYandexAdapter(cfg, &http.Client{Transport: transport}, &serviceMetrics{})
	adapter.now = func() time.Time { return time.Unix(1_700_000_000, 0) }
	adapter.cooldownJitter = func(maximum time.Duration) time.Duration {
		if maximum != maxProviderCooldownJitter {
			t.Fatalf("jitter bound=%s, want %s", maximum, maxProviderCooldownJitter)
		}
		return 125 * time.Millisecond
	}
	var sleepDuration time.Duration
	adapter.sleep = func(_ context.Context, delay time.Duration) error {
		sleepDuration = delay
		waited.Store(true)
		return nil
	}

	_, err := adapter.Routes(context.Background(), validProviderRequest())
	if err == nil || normalizeProviderError(err).Code != "PROVIDER_RATE_LIMITED" {
		t.Fatalf("Routes() error=%v, want provider rate limit", err)
	}
	if _, err := adapter.Suggest(context.Background(), "Moscow", "en_US", 1); err != nil {
		t.Fatal(err)
	}
	if geocoderBeforeWait.Load() {
		t.Fatal("geocoder reached the provider before the router cooldown wait")
	}
	if sleepDuration != 2*time.Second+125*time.Millisecond {
		t.Fatalf("shared cooldown wait=%s, want 2.125s", sleepDuration)
	}
}

func TestSharedCooldownClampsProviderDelayAndJitter(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	cooldown := &sharedCooldown{}
	cooldown.Extend(now, 24*time.Hour)
	delay := cooldown.WaitDuration(now, func(time.Duration) time.Duration {
		return maxProviderCooldownJitter + time.Hour
	})
	if want := maxProviderCooldown + maxProviderCooldownJitter; delay != want {
		t.Fatalf("bounded delay=%s, want %s", delay, want)
	}
	if got := parseRetryAfter("999999999999999999", now); got != maxProviderCooldown {
		t.Fatalf("huge Retry-After=%s, want %s", got, maxProviderCooldown)
	}
	if delay := cooldown.WaitDuration(now.Add(maxProviderCooldown+time.Second), nil); delay != 0 {
		t.Fatalf("expired cooldown returned %s", delay)
	}
}

func TestCredentialFaultLatchFailsReadinessUntilAuthorizedSuccess(t *testing.T) {
	fixture := string(readContractFixture(t))
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var calls atomic.Int32
			transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
				if calls.Add(1) == 1 {
					return response(status, `{"message":"sensitive credential details"}`), nil
				}
				return response(http.StatusOK, fixture), nil
			})
			cfg := testConfig()
			cfg.MaxRetries = 0
			adapter := newYandexAdapter(cfg, &http.Client{Transport: transport}, &serviceMetrics{})

			_, err := adapter.Routes(context.Background(), validProviderRequest())
			if err == nil || normalizeProviderError(err).Code != "PROVIDER_AUTHENTICATION_FAILED" {
				t.Fatalf("Routes() error=%v, want sanitized authentication failure", err)
			}
			if !errors.Is(adapter.Ready(), errCredentialFault) {
				t.Fatalf("Ready()=%v, want latched credential fault", adapter.Ready())
			}
			if _, err := adapter.Routes(context.Background(), validProviderRequest()); err != nil {
				t.Fatalf("authorized recovery probe failed: %v", err)
			}
			if err := adapter.Ready(); err != nil {
				t.Fatalf("successful authorized probe did not clear readiness latch: %v", err)
			}
		})
	}
}

func TestCredentialSuccessOnlyClearsItsOwnScope(t *testing.T) {
	fixture := string(readContractFixture(t))
	var routerCalls atomic.Int32
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Hostname() == "geocode-maps.yandex.ru" {
			return response(http.StatusOK, `{}`), nil
		}
		if routerCalls.Add(1) == 1 {
			return response(http.StatusUnauthorized, `{}`), nil
		}
		return response(http.StatusOK, fixture), nil
	})
	cfg := testConfig()
	cfg.MaxRetries = 0
	adapter := newYandexAdapter(cfg, &http.Client{Transport: transport}, &serviceMetrics{})

	_, _ = adapter.Routes(context.Background(), validProviderRequest())
	if !errors.Is(adapter.Ready(), errCredentialFault) {
		t.Fatal("router credential failure did not latch readiness")
	}
	if _, err := adapter.Suggest(context.Background(), "Moscow", "en_US", 1); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(adapter.Ready(), errCredentialFault) {
		t.Fatal("geocoder success incorrectly cleared router credential fault")
	}
	if _, err := adapter.Routes(context.Background(), validProviderRequest()); err != nil {
		t.Fatal(err)
	}
	if err := adapter.Ready(); err != nil {
		t.Fatalf("router recovery did not clear its credential fault: %v", err)
	}
}

func receiveDuration(t *testing.T, values <-chan time.Duration) time.Duration {
	t.Helper()
	select {
	case value := <-values:
		return value
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for cooldown")
		return 0
	}
}
