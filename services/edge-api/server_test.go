package main

import (
	"fmt"
	"net/http/httptest"
	"net/netip"
	"testing"
)

func TestClientIPWalksTrustedProxyChainRightToLeft(t *testing.T) {
	server := &apiServer{proxyRanges: []netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/8"),
		netip.MustParsePrefix("192.0.2.0/24"),
	}}
	request := httptest.NewRequest("GET", "http://edge/api/v1/me", nil)
	request.RemoteAddr = "10.0.0.9:43120"
	request.Header.Set("X-Forwarded-For", "203.0.113.250, 198.51.100.7, 192.0.2.12")

	if got := server.clientIP(request); got != "198.51.100.7" {
		t.Fatalf("spoofed leftmost XFF address was trusted: got %q", got)
	}
}

func TestRouteTemplateHasBoundedCardinalityForUntrustedPaths(t *testing.T) {
	for index := 0; index < 1_000; index++ {
		path := fmt.Sprintf("/api/v1/route-searches/customer-address-%d", index)
		if got := routeTemplate(path); got != "/api/v1/route-searches/{searchId}" {
			t.Fatalf("unexpected template for %q: %q", path, got)
		}
	}
	if got := routeTemplate("/person@example.com/private"); got != "/unmatched" {
		t.Fatalf("raw unmatched path was retained: %q", got)
	}
}

func TestClientIPIgnoresForwardingFromUntrustedPeer(t *testing.T) {
	server := &apiServer{proxyRanges: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}}
	request := httptest.NewRequest("GET", "http://edge/api/v1/me", nil)
	request.RemoteAddr = "198.51.100.22:43120"
	request.Header.Set("X-Forwarded-For", "203.0.113.99")

	if got := server.clientIP(request); got != "198.51.100.22" {
		t.Fatalf("untrusted peer controlled effective client IP: got %q", got)
	}
}

func TestClientIPFailsClosedOnMalformedTrustedChain(t *testing.T) {
	server := &apiServer{proxyRanges: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}}
	request := httptest.NewRequest("GET", "http://edge/api/v1/me", nil)
	request.RemoteAddr = "10.0.0.9:43120"
	request.Header.Set("X-Forwarded-For", "198.51.100.7, malformed")

	if got := server.clientIP(request); got != "10.0.0.9" {
		t.Fatalf("malformed chain must fall back to authenticated peer: got %q", got)
	}
}
