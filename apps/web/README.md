# Пробок.Нет web

Production Next.js client for route planning, route comparison and the protected operations view. The browser talks only to `edge-api`; the Yandex Router/server credential is never accepted by this package.

## Runtime contract

The typed contract is generated from `../../docs/api/openapi.yaml`:

```bash
npm run generate:api
```

The client uses these public endpoints with `credentials: include`:

- `GET /api/v1/geosuggest?q=&lang=ru_RU|en_US&limit=`;
- `POST /api/v1/route-searches` with `Idempotency-Key` and `X-Request-ID`;
- `GET /api/v1/route-searches/{searchId}`;
- `GET /api/v1/route-searches/{searchId}/events` as SSE;
- `DELETE /api/v1/route-searches/{searchId}`;
- `GET /api/v1/me` before rendering admin data;
- `GET /api/v1/admin/overview`, which must enforce the `admin` role on the server.

Incoming JSON is validated with Zod. SSE reconnects with `Last-Event-ID`; after four interrupted connections the UI switches to bounded polling. A new search aborts the previous browser stream, and cancellation is propagated to edge-api.

## Configuration

Copy `.env.example` to `.env.local` for local development.

| Variable | Behaviour |
| --- | --- |
| `NEXT_PUBLIC_EDGE_API_BASE_URL` | Required unless demo mode is explicitly enabled. HTTPS is required except on localhost. Missing/invalid configuration fails closed in the UI. |
| `NEXT_PUBLIC_YANDEX_MAPS_API_KEY` | Optional origin-restricted JavaScript API browser key. If absent or loading fails, an accessible schematic map remains available. |
| `NEXT_PUBLIC_2GIS_MAPGL_API_KEY` | Optional origin-restricted 2GIS MapGL browser key used only by the explicit 2GIS traffic renderer. Keep it separate from the production server routing credential. |
| `NEXT_PUBLIC_DEMO_MODE` | Exact value `true` enables synthetic fixtures. Default and production manifests set it to `false`. |
| `GREENROUTE_PROVIDER_MODE` / `GREENROUTE_ADDRESS_PROVIDER_MODE` | Optional privacy-safe runtime diagnostics. Server API cards do not trust these labels; they use the authorized admin overview contract. |
| `GREENROUTE_*_CONFIGURED` | Optional privacy-safe diagnostics retained for deployment inspection. They never establish that a server API is active. |
| `GREENROUTE_YANDEX_TRAFFIC_AVAILABLE` | Enables the official Yandex JS API v3 traffic package after a compatible paid plan is connected. Defaults to `false`; failures fall back to traffic-off mode. |

`NEXT_PUBLIC_*` values are public, read when the web container starts, and rendered into the HTML runtime configuration. The same immutable image can therefore be promoted between environments. Never provide a Yandex Router API key, OAuth client secret or another server credential through these variables. Restrict the map key to the deployed browser origins in the provider console.

The operations page builds server-side API cards from the authorized
`apiIntegrations` array returned by `/api/v1/admin/overview`. Each entry contains
only provider, product, exact API version, capability, primary/fallback role and
active/standby selection state. The JavaScript API v3 card is separate because
it is a browser integration. Geosuggest is not shown as active: the current
coordinate-bearing address contract uses HTTP Geocoder APIs.

## Local workflow

Requires Node.js 24.15+ LTS and npm 11.

```bash
npm ci
npm run dev
npm run lint
npm run typecheck
npm test
npm run build
npm run test:e2e
```

Playwright starts an explicit fixture-mode development server on port 3100. Production configuration never silently falls back to fixtures.

## Container

Use `apps/web` as the build context. Supply public configuration when the container starts; it is intentionally absent from the image build.

```bash
docker build \
  -t greenroute-web:local apps/web
docker run --read-only --tmpfs /tmp --cap-drop ALL -p 3000:3000 \
  -e NEXT_PUBLIC_EDGE_API_BASE_URL=https://edge.example.com \
  -e NEXT_PUBLIC_YANDEX_MAPS_API_KEY=restricted-browser-key \
  -e NEXT_PUBLIC_2GIS_MAPGL_API_KEY=restricted-mapgl-browser-key \
  -e NEXT_PUBLIC_DEMO_MODE=false \
  greenroute-web:local
```

The standalone image runs as UID/GID 1001, exposes port 3000 and reports liveness at `GET /api/health`.

## Security and privacy notes

- A per-request nonce is generated in `src/proxy.ts`; Next.js hydration scripts receive it automatically. Production does not enable `unsafe-eval` or `unsafe-inline` for scripts.
- Route API responses, coordinates and authenticated HTML are never cached by the service worker. The PWA caches only the offline page, manifest, icons and versioned Next static assets.
- Authentication state is carried by the edge-api cookie/session mechanism. The client does not persist access tokens.
- The admin UI performs an optimistic role gate, but edge-api remains the authorization authority and must return 401/403 independently.
- Segment colours are always labelled as a Пробок.Нет estimate. `UNKNOWN` remains distinct and is excluded from the free-flow percentage denominator.
