# ADR-013: Traffic layer provider selection

Status: Accepted  
Date: 2026-08-05

## Context

Пробок.Нет renders provider-neutral route geometry, but a live traffic overlay is
provider-owned map data. The overlay must not imply that Пробок.Нет's own
segment estimate is an official congestion classification.

Yandex JavaScript API v3 documents `YMapTrafficLayer` and
`YMapTrafficEventsLayer` in `@yandex/ymaps3-layers-extra`. Yandex explicitly
states that these layers require a paid tariff. The current localhost Yandex
JavaScript API key uses the free daily allowance and therefore has no confirmed
traffic-layer entitlement.

2GIS MapGL JS API documents its traffic layer through the `trafficOn` map
option and `showTraffic`, `hideTraffic`, and `isTrafficOn` methods. The current
localhost demo key includes MapGL JS API access.

The two providers use different base-map renderers and attribution. Rendering
2GIS traffic data over a Yandex base map is neither a documented integration nor
a reliable way to preserve provider attribution.

## Decision

1. Model the visual traffic selection as one enum: `off | yandex | 2gis`.
   Two traffic layers cannot be active simultaneously by construction.
2. Keep Yandex JavaScript API v3 as the default interactive base map when the
   selection is `off` or `yandex`.
3. Enable the Yandex traffic choice only when the safe runtime capability flag
   `GREENROUTE_YANDEX_TRAFFIC_AVAILABLE=true` is explicitly configured. Import
   only the documented `@yandex/ymaps3-layers-extra` package. If the import or
   layer initialization fails, return the selection to `off` and show a clear
   unavailable status.
4. When `2gis` is selected, switch the complete map renderer to 2GIS MapGL and
   initialize it with `trafficOn: true`. Render the same provider-neutral route
   geometry and markers on the MapGL map. Switching back destroys the MapGL
   instance and returns to Yandex v3 without a traffic overlay.
5. A missing entitlement or browser key is represented as a disabled option
   with its reason; the UI never fabricates a traffic layer.
6. The map-overlay selection does not change the routing provider. Route ETA,
   confidence, and Пробок.Нет segment estimates remain separately labelled.
7. Browser map keys are supplied as runtime HTML configuration, never compiled
   into a bundle. Production must use a dedicated, origin-restricted 2GIS MapGL
   browser key instead of reusing a server routing credential. The localhost
   launcher may map the current demo credential into the browser-only variable
   solely for local evaluation.

## Consequences

- The UI is truthful for the current Yandex free tariff while being ready for a
  paid entitlement through configuration only.
- 2GIS traffic can be inspected now without undocumented tile scraping.
- Provider attribution and map controls remain owned by the active renderer.
- Switching providers recreates the map instance; route selection and geometry
  remain in React state and are redrawn after initialization.

## References

- [Yandex JavaScript API v3 traffic example](https://yandex.ru/maps-api/docs/js-api/examples/cases/traffic.html)
- [2GIS MapGL traffic control](https://docs.2gis.com/en/mapgl/map/configuration/controls)
- [2GIS on-premise traffic proxy architecture](https://docs.2gis.com/on-premise-api-platform/architecture/api-platform/trafficproxy)

