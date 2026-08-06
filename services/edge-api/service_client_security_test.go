package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestServiceClientNeverForwardsTokenAcrossRedirect(t *testing.T) {
	for _, stream := range []bool{false, true} {
		name := "request"
		if stream {
			name = "stream"
		}
		t.Run(name, func(t *testing.T) {
			var targetCalls atomic.Int32
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				targetCalls.Add(1)
				if token := r.Header.Get("Authorization"); token != "" {
					t.Errorf("redirect target received Authorization=%q", token)
				}
				w.WriteHeader(http.StatusOK)
			}))
			defer target.Close()

			redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if token := r.Header.Get("Authorization"); token != "Bearer internal-secret" {
					t.Errorf("configured service received Authorization=%q", token)
				}
				http.Redirect(w, r, target.URL+"/token-sink", http.StatusTemporaryRedirect)
			}))
			defer redirector.Close()

			client, err := newServiceClient(redirector.URL, redirector.URL, "internal-secret")
			if err != nil {
				t.Fatal(err)
			}
			response, err := client.do(context.Background(), client.orchestrator, http.MethodGet, "/redirect", "request-id", nil, stream)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != http.StatusTemporaryRedirect {
				t.Fatalf("status=%d, want redirect response", response.StatusCode)
			}
			if calls := targetCalls.Load(); calls != 0 {
				t.Fatalf("redirect target was called %d times", calls)
			}
		})
	}
}

func TestServiceClientRedirectPolicyReturnsOriginalResponse(t *testing.T) {
	if err := rejectInternalRedirect(nil, nil); err != http.ErrUseLastResponse {
		t.Fatalf("redirect policy error=%v, want http.ErrUseLastResponse", err)
	}
}

func TestServiceClientTransportIgnoresAmbientProxy(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://attacker.invalid:8080")
	t.Setenv("HTTPS_PROXY", "http://attacker.invalid:8080")
	if proxy := newInternalServiceTransport().Proxy; proxy != nil {
		t.Fatal("internal service transport configured an ambient proxy function")
	}
}
