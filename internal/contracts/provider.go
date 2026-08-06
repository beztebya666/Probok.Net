package contracts

import "github.com/greenroute/greenroute/internal/domain"

const InternalContractVersion = "v1"

type ProviderRouteRequest struct {
	RequestID     string            `json:"requestId"`
	Origin        domain.GeoPoint   `json:"origin"`
	Destination   domain.GeoPoint   `json:"destination"`
	Waypoints     []domain.GeoPoint `json:"waypoints,omitempty"`
	Traffic       bool              `json:"traffic"`
	Alternatives  int               `json:"alternatives"`
	AvoidTolls    bool              `json:"avoidTolls"`
	AvoidUnpaved  bool              `json:"avoidUnpaved"`
	DepartureUnix int64             `json:"departureUnix,omitempty"`
	AvoidZones    []AvoidZone       `json:"avoidZones,omitempty"`
	RequestBudget int               `json:"requestBudget"`
	DeadlineMS    int               `json:"deadlineMs"`
}

type ProviderRouteInput struct {
	RequestID     *string           `json:"requestId"`
	Origin        *domain.GeoPoint  `json:"origin"`
	Destination   *domain.GeoPoint  `json:"destination"`
	Waypoints     []domain.GeoPoint `json:"waypoints,omitempty"`
	Traffic       *bool             `json:"traffic"`
	Alternatives  *int              `json:"alternatives"`
	AvoidTolls    *bool             `json:"avoidTolls"`
	AvoidUnpaved  *bool             `json:"avoidUnpaved"`
	DepartureUnix int64             `json:"departureUnix,omitempty"`
	AvoidZones    []AvoidZone       `json:"avoidZones,omitempty"`
	RequestBudget *int              `json:"requestBudget"`
	DeadlineMS    *int              `json:"deadlineMs"`
}

func (input ProviderRouteInput) Request() (ProviderRouteRequest, string) {
	switch {
	case input.RequestID == nil:
		return ProviderRouteRequest{}, "requestId"
	case input.Origin == nil:
		return ProviderRouteRequest{}, "origin"
	case input.Destination == nil:
		return ProviderRouteRequest{}, "destination"
	case input.Traffic == nil:
		return ProviderRouteRequest{}, "traffic"
	case input.Alternatives == nil:
		return ProviderRouteRequest{}, "alternatives"
	case input.AvoidTolls == nil:
		return ProviderRouteRequest{}, "avoidTolls"
	case input.AvoidUnpaved == nil:
		return ProviderRouteRequest{}, "avoidUnpaved"
	case input.RequestBudget == nil:
		return ProviderRouteRequest{}, "requestBudget"
	case input.DeadlineMS == nil:
		return ProviderRouteRequest{}, "deadlineMs"
	}
	return ProviderRouteRequest{
		RequestID: *input.RequestID, Origin: *input.Origin, Destination: *input.Destination,
		Waypoints: append([]domain.GeoPoint(nil), input.Waypoints...), Traffic: *input.Traffic,
		Alternatives: *input.Alternatives, AvoidTolls: *input.AvoidTolls, AvoidUnpaved: *input.AvoidUnpaved,
		DepartureUnix: input.DepartureUnix, AvoidZones: append([]AvoidZone(nil), input.AvoidZones...),
		RequestBudget: *input.RequestBudget, DeadlineMS: *input.DeadlineMS,
	}, ""
}

type AvoidZone struct {
	Points []domain.GeoPoint `json:"points"`
}

type ProviderRouteResponse struct {
	Candidates             []domain.RouteCandidate `json:"candidates"`
	RequestsUsed           int                     `json:"requestsUsed"`
	EstimatedBillableUnits int                     `json:"estimatedBillableUnits"`
	BudgetRemaining        int                     `json:"budgetRemaining"`
	Warnings               []string                `json:"warnings,omitempty"`
	Degraded               bool                    `json:"degraded"`
}

type ProviderCapabilities struct {
	ContractVersion           string           `json:"contractVersion"`
	Provider                  string           `json:"provider"`
	Mode                      string           `json:"mode"`
	APIIntegrations           []APIIntegration `json:"apiIntegrations,omitempty"`
	MaxAlternatives           int              `json:"maxAlternatives"`
	MaxWaypoints              int              `json:"maxWaypoints"`
	RealtimeTraffic           bool             `json:"realtimeTraffic"`
	TrafficDisabledBaseline   bool             `json:"trafficDisabledBaseline"`
	DepartureTime             bool             `json:"departureTime"`
	AvoidZones                bool             `json:"avoidZones"`
	Geosuggest                bool             `json:"geosuggest"`
	Coverage                  []string         `json:"coverage"`
	ExperimentalSources       bool             `json:"experimentalSources"`
	DataStorageAllowed        bool             `json:"dataStorageAllowed"`
	RawResponseCacheTTLSecond int              `json:"rawResponseCacheTtlSeconds"`
}

// APIIntegration is privacy-safe runtime metadata for the exact documented API
// selected by the provider adapter. It deliberately carries no key, account,
// tenant, quota-consumption, request URL, or other credential-derived value.
// Role describes selection order, while State describes whether the adapter
// currently selects the integration or keeps it as an automatic fallback.
type APIIntegration struct {
	ID         string `json:"id"`
	Provider   string `json:"provider"`
	Product    string `json:"product"`
	APIVersion string `json:"apiVersion"`
	Capability string `json:"capability"`
	Role       string `json:"role"`
	State      string `json:"state"`
}

type GeosuggestResponse struct {
	Suggestions []domain.GeoSuggestion `json:"suggestions"`
}

type InternalSearchStartResponse struct {
	Result domain.RouteSearchResult `json:"result"`
}
