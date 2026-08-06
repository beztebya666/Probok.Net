package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
)

type principal struct {
	UserID          string    `json:"userId"`
	DisplayName     string    `json:"displayName"`
	Roles           []string  `json:"roles"`
	Authenticated   bool      `json:"authenticated"`
	ExpiresAt       time.Time `json:"-"`
	OwnerIDs        []string  `json:"-"`
	AbuseSubjectIDs []string  `json:"-"`
}

type principalContextKey struct{}

type authenticator struct {
	anonymous      bool
	anonymousAdmin bool
	adminGroup     string
	verifier       *oidc.IDTokenVerifier
	abuseKeys      [][]byte
}

func newAuthenticator(ctx context.Context, cfg config) (*authenticator, error) {
	auth := &authenticator{anonymous: cfg.AnonymousUsage, anonymousAdmin: cfg.LocalAnonymousAdmin, adminGroup: cfg.OIDCAdminGroup}
	currentKey := cfg.AbuseHashKey
	if currentKey == "" {
		currentKey = "greenroute-local-abuse-pseudonym-key"
	}
	auth.abuseKeys = append(auth.abuseKeys, []byte(currentKey))
	if cfg.AbusePreviousHashKey != "" && cfg.AbusePreviousHashKey != currentKey {
		auth.abuseKeys = append(auth.abuseKeys, []byte(cfg.AbusePreviousHashKey))
	}
	if cfg.OIDCIssuerURL == "" {
		if cfg.AnonymousUsage {
			return auth, nil
		}
		return nil, errors.New("OIDC issuer missing")
	}
	provider, err := oidc.NewProvider(ctx, cfg.OIDCIssuerURL)
	if err != nil {
		return nil, err
	}
	auth.verifier = provider.Verifier(&oidc.Config{ClientID: cfg.OIDCClientID, SupportedSigningAlgs: []string{oidc.RS256, oidc.ES256}})
	return auth, nil
}

func (a *authenticator) authenticate(r *http.Request, clientIP string) (principal, error) {
	authorization := strings.TrimSpace(r.Header.Get("Authorization"))
	if authorization == "" && a.anonymous {
		identifiers := a.abusePseudonyms("anonymous-ip", clientIP)
		roles := []string{"user"}
		displayName := "Local user"
		if a.anonymousAdmin {
			roles = append(roles, "admin")
			displayName = "Local admin"
		}
		return principal{
			UserID:          "anonymous:" + identifiers[0],
			OwnerIDs:        prefixedPseudonyms("owner:", a.abusePseudonyms("owner-anonymous", clientIP)),
			AbuseSubjectIDs: a.abusePseudonyms("rate-anonymous", clientIP),
			DisplayName:     displayName, Roles: roles, Authenticated: false,
		}, nil
	}
	if !strings.HasPrefix(authorization, "Bearer ") || len(authorization) <= len("Bearer ") {
		return principal{}, errors.New("missing bearer token")
	}
	if a.verifier == nil {
		return principal{}, errors.New("token verifier unavailable")
	}
	token, err := a.verifier.Verify(r.Context(), strings.TrimPrefix(authorization, "Bearer "))
	if err != nil {
		return principal{}, errors.New("invalid access token")
	}
	claims := struct {
		Subject           string   `json:"sub"`
		Name              string   `json:"name"`
		PreferredUsername string   `json:"preferred_username"`
		Roles             []string `json:"roles"`
		Groups            []string `json:"groups"`
	}{}
	if err := token.Claims(&claims); err != nil || claims.Subject == "" {
		return principal{}, errors.New("required token claims missing")
	}
	roles := []string{"user"}
	for _, role := range claims.Roles {
		if role == "admin" {
			roles = append(roles, "admin")
		}
	}
	if slices.Contains(claims.Groups, a.adminGroup) {
		roles = append(roles, "admin")
	}
	name := claims.Name
	if name == "" {
		name = claims.PreferredUsername
	}
	return principal{
		UserID: claims.Subject, OwnerIDs: prefixedPseudonyms("owner:", a.abusePseudonyms("owner-user", claims.Subject)),
		AbuseSubjectIDs: a.abusePseudonyms("rate-user", claims.Subject),
		DisplayName:     name, Roles: uniqueStrings(roles), Authenticated: true, ExpiresAt: token.Expiry,
	}, nil
}

func prefixedPseudonyms(prefix string, values []string) []string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = prefix + value
	}
	return result
}

func withPrincipal(ctx context.Context, value principal) context.Context {
	return context.WithValue(ctx, principalContextKey{}, value)
}

func principalFrom(ctx context.Context) principal {
	value, _ := ctx.Value(principalContextKey{}).(principal)
	return value
}

func hasRole(value principal, role string) bool { return slices.Contains(value.Roles, role) }

func (a *authenticator) abusePseudonyms(namespace, value string) []string {
	result := make([]string, 0, len(a.abuseKeys))
	for _, key := range a.abuseKeys {
		hash := hmac.New(sha256.New, key)
		_, _ = hash.Write([]byte(namespace))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(value))
		result = append(result, hex.EncodeToString(hash.Sum(nil)[:16]))
	}
	return result
}
