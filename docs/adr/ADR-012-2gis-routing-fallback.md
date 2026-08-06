# ADR-012: Official 2GIS routing fallback

Status: accepted, 2026-08-05.

## Context

The Yandex Route Details product is not yet enabled for the local project, while
the current 2GIS demo key has Routing and Geocoder services. The product needs
real route geometry without scraping or exposing a server credential to the
browser.

## Decision

`PROVIDER_MODE=2gis` selects a provider-neutral adapter inside the historically
named `provider-yandex` process. It calls only the documented endpoints:

- `POST https://routing.api.2gis.com/routing/7.0.0/global`;
- `GET https://catalog.api.2gis.com/3.0/items/geocode`.

Routing uses detailed WKT output, at most ten points and three returned routes,
documented traffic modes, road filters, hard polygon exclusions, and payment
information. The server key is accepted only from `DGIS_API_KEY`; redirects,
ambient proxies, dynamic provider URLs, and raw response persistence are denied.

Address resolution is selected independently. `ADDRESS_PROVIDER_MODE=auto`
uses the point-bearing Yandex HTTP Geocoder when `YANDEX_GEOCODER_API_KEY` is
configured and otherwise uses 2GIS Geocoder 3.0. In auto mode, 2GIS Geocoder is
also a bounded fallback when Yandex returns an error or no matches. Caller
cancellation is never retried; each resolver keeps its own documented quota
gate, and a failed opportunistic fallback cannot turn a valid empty primary
response into a server error. The 2GIS request carries one official `location`
ranking hint (Moscow by default) without rewriting an explicitly named city.
Capabilities publish the selected address provider and a privacy-safe
`apiIntegrations` list: 2GIS Routing 7.0.0, Yandex HTTP Geocoder v1 as primary,
and 2GIS Geocoder 3.0 as standby. It contains no key or account identifier.
Aggregate fallback attempts are observable only as `primary_error` or
`primary_empty`; queries and coordinates are never metric labels.

The current route quota is protected by a configurable rolling pre-egress gate
(default 5 objects/minute). A full gate returns `429` with `Retry-After` without
spending internal request budget. `DGIS_MONTHLY_LIMIT` is surfaced for operations,
but 2GIS Platform Manager remains the authoritative cross-replica hard limit.

## Traffic truthfulness

2GIS documents that current or statistical traffic affects route calculation,
but does not expose a supported detailed congestion level. Response geometry
colors are therefore ignored. Normalized segments remain `UNKNOWN`; the route
is labeled `REALTIME` or `FORECAST` only at data-source level. A traffic-disabled
comparison uses documented shortest-distance mode and is scored only after the
orchestrator's geometry-similarity check.

## Consequences

- The localhost product can build real routes and resolve addresses now.
- Alternatives for one point set are estimated as one billable unit, matching
  current 2GIS documentation; Platform Manager statistics remain authoritative.
- Directory/deployment names stay unchanged to avoid broad operational churn.
- 2GIS Geocoder is an address-to-coordinate search, not branded as the separate
  Suggest API; true autocomplete can be added later behind the same neutral DTO.

## Primary sources

- https://docs.2gis.com/en/api/navigation/routing/overview
- https://docs.2gis.com/en/api/navigation/routing/reference/routing
- https://docs.2gis.com/en/api/navigation/routing/examples/routing
- https://docs.2gis.com/api/search/geocoder/reference/3.0/items/geocode
- https://docs.2gis.com/platform-manager/subscription/pricing
