package main

import (
	"context"
	"net/http"
	"testing"
)

func TestExplicitLocalAnonymousPrincipalHasAdminRole(t *testing.T) {
	auth, err := newAuthenticator(context.Background(), config{
		Environment:         "local",
		AnonymousUsage:      true,
		LocalAnonymousAdmin: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodGet, "http://localhost/api/v1/me", nil)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := auth.authenticate(request, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if identity.Authenticated {
		t.Fatal("local anonymous identity must remain unauthenticated")
	}
	if !hasRole(identity, "user") || !hasRole(identity, "admin") {
		t.Fatalf("roles=%v, want user and admin", identity.Roles)
	}
	if identity.DisplayName != "Local admin" {
		t.Fatalf("displayName=%q, want Local admin", identity.DisplayName)
	}
}
