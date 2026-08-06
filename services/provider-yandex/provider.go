package main

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/greenroute/greenroute/internal/contracts"
)

var (
	errBudgetExhausted = errors.New("provider request budget exhausted")
	errCircuitOpen     = errors.New("provider circuit breaker is open")
	errBulkheadFull    = errors.New("provider concurrency bulkhead is full")
	errCredentialFault = errors.New("provider credential fault is latched")
	errUnsupported     = errors.New("operation is not supported by active provider")
)

const (
	apiCapabilityRouting       = "routing"
	apiCapabilityAddressSearch = "address-search"
	apiRolePrimary             = "primary"
	apiRoleFallback            = "fallback"
	apiStateActive             = "active"
	apiStateStandby            = "standby"
)

type providerError struct {
	Code       string
	Message    string
	HTTPStatus int
	Retryable  bool
	RetryAfter time.Duration
	Cause      error
}

func (e *providerError) Error() string {
	return e.Code
}

func (e *providerError) Unwrap() error { return e.Cause }

func serviceError(code, message string, status int, retryable bool, cause error) *providerError {
	return &providerError{Code: code, Message: message, HTTPStatus: status, Retryable: retryable, Cause: cause}
}

type requestBudget struct {
	remaining int
	used      int
}

func newRequestBudget(limit int) *requestBudget {
	return &requestBudget{remaining: limit}
}

func (b *requestBudget) Consume() bool {
	if b.remaining <= 0 {
		return false
	}
	b.remaining--
	b.used++
	return true
}

func (b *requestBudget) Used() int      { return b.used }
func (b *requestBudget) Remaining() int { return b.remaining }

type capabilityDocument struct {
	contracts.ProviderCapabilities
	VerifiedAt                  string            `json:"verifiedAt"`
	OfficialDocumentation       []string          `json:"officialDocumentation"`
	OfficialEndpoint            string            `json:"officialEndpoint,omitempty"`
	AddressSearchProvider       string            `json:"addressSearchProvider,omitempty"`
	AddressSearchEndpoint       string            `json:"addressSearchEndpoint,omitempty"`
	MaxRoutesPerRequest         int               `json:"maxRoutesPerRequest"`
	RequestsPerSecond           int               `json:"requestsPerSecond"`
	RequestsPerMinute           int               `json:"requestsPerMinute,omitempty"`
	DailyRequestLimit           *int              `json:"dailyRequestLimit"`
	MonthlyRequestLimit         *int              `json:"monthlyRequestLimit,omitempty"`
	DailyLimitContractDependent bool              `json:"dailyLimitContractDependent"`
	AvoidTolls                  bool              `json:"avoidTolls"`
	AvoidUnpaved                string            `json:"avoidUnpaved"`
	Billing                     billingCapability `json:"billing"`
	Storage                     storageCapability `json:"storage"`
	DataModificationAllowed     bool              `json:"dataModificationAllowed"`
	Licenses                    licenseCapability `json:"licenses"`
	Limitations                 []string          `json:"limitations"`
	ExperimentalRequested       bool              `json:"experimentalSourcesRequested"`
}

type billingCapability struct {
	Unit                string `json:"unit"`
	MultipleRoutesCount string `json:"multipleRoutesCount"`
}

type storageCapability struct {
	Standard string `json:"standard"`
	Extended string `json:"extended"`
}

type licenseCapability struct {
	BasicName    string `json:"basicName"`
	AdvancedName string `json:"advancedName"`
	FreeTier     string `json:"freeTier"`
}

type Provider interface {
	Routes(context.Context, contracts.ProviderRouteRequest) (contracts.ProviderRouteResponse, error)
	Suggest(context.Context, string, string, int) (contracts.GeosuggestResponse, error)
	Capabilities() capabilityDocument
	Ready() error
}

func normalizeProviderError(err error) *providerError {
	if err == nil {
		return nil
	}
	var target *providerError
	if errors.As(err, &target) {
		return target
	}
	switch {
	case errors.Is(err, errBudgetExhausted):
		return serviceError("PROVIDER_BUDGET_EXHAUSTED", "provider request budget exhausted", http.StatusTooManyRequests, false, err)
	case errors.Is(err, errCircuitOpen):
		return serviceError("PROVIDER_UNAVAILABLE", "provider is temporarily unavailable", http.StatusServiceUnavailable, true, err)
	case errors.Is(err, errBulkheadFull):
		return serviceError("PROVIDER_BUSY", "provider concurrency limit reached", http.StatusServiceUnavailable, true, err)
	case errors.Is(err, errUnsupported):
		return serviceError("PROVIDER_CAPABILITY_UNSUPPORTED", "capability is not supported by the active provider", http.StatusNotImplemented, false, err)
	case errors.Is(err, context.DeadlineExceeded):
		return serviceError("PROVIDER_TIMEOUT", "provider request timed out", http.StatusGatewayTimeout, true, err)
	case errors.Is(err, context.Canceled):
		return serviceError("REQUEST_CANCELLED", "request was cancelled", 499, false, err)
	default:
		return serviceError("PROVIDER_ERROR", "provider request failed", http.StatusBadGateway, true, err)
	}
}
