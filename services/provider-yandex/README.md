# provider-yandex

Despite the historical directory name, this binary is a provider-neutral
boundary with three explicit modes: `yandex`, `2gis`, and `stub`.

For the current 2GIS demo subscription:

```powershell
$env:PROVIDER_MODE = "2gis"
$env:DGIS_API_KEY = (Get-Content -Raw C:\secure\dgis-key.txt).Trim()
$env:DGIS_RATE_LIMIT_PER_MINUTE = "5"
$env:DGIS_MONTHLY_LIMIT = "1000"
go run ./services/provider-yandex
```

`2gis` routes through the fixed official Routing 7.0.0 endpoint and resolves
addresses through official Geocoder 3.0. Detailed congestion classes are not a
documented response field, so normalized route segments remain `UNKNOWN`.

Изолированный stateless adapter официальных Yandex Route Details API и HTTP
Geocoder. Сервис принимает только provider-neutral DTO, не сохраняет и не
логирует координаты, сырые provider payload или API key.

## Локальный запуск

Stub включается только явно:

```powershell
$env:PROVIDER_MODE = "stub"
go run ./services/provider-yandex
```

Проверка:

```powershell
Invoke-RestMethod http://127.0.0.1:8082/health/ready
Invoke-RestMethod http://127.0.0.1:8082/internal/v1/capabilities
```

Пример маршрута:

```powershell
$body = @{
  requestId = "demo-001"
  origin = @{ latitude = 55.751244; longitude = 37.618423 }
  destination = @{ latitude = 55.790000; longitude = 37.700000 }
  traffic = $true
  alternatives = 2
  avoidTolls = $false
  avoidUnpaved = $false
  requestBudget = 3
  deadlineMs = 5000
} | ConvertTo-Json -Depth 8

Invoke-RestMethod -Method Post `
  -Uri http://127.0.0.1:8082/internal/v1/routes `
  -ContentType application/json `
  -Body $body
```

Real adapter (ключ не помещать в shell history/`.env`; пример показывает только
имя секрета):

```powershell
$env:PROVIDER_MODE = "yandex"
$env:YANDEX_ROUTER_API_KEY = (Get-Content -Raw C:\secure\yandex-router-key.txt).Trim()
$env:YANDEX_GEOCODER_API_KEY = (Get-Content -Raw C:\secure\yandex-geocoder-key.txt).Trim()
go run ./services/provider-yandex
```

Ключ Geocoder должен иметь отдельный пакет/лицензию. Если
`YANDEX_GEOCODER_API_KEY` не задан, сервис использует router key как fallback;
это работает только если один ключ имеет оба entitlement.

## Endpoints

| Endpoint | Назначение |
|---|---|
| `POST /internal/v1/routes` | Нормализованные route candidates |
| `GET /internal/v1/capabilities` | Фактическая capability/licensing matrix |
| `GET /internal/v1/geosuggest?q=...&lang=ru_RU&limit=7` | Точки адресов из официального HTTP Geocoder; в stub — синтетические точки |
| `GET /health/live` (`/healthz`) | Liveness |
| `GET /health/ready` (`/readyz`) | Readiness, включая open circuit/outage stub |
| `GET /metrics` | Prometheus text format без coordinate labels |

Ответы содержат `Cache-Control: no-store`; provider errors санитизируются.
`requestsUsed` считает outbound attempts, а `estimatedBillableUnits` — число
маршрутов успешного ответа. Последнее является оценкой: статистика/договор Яндекса
имеют приоритет.

## Конфигурация

| Variable | Default | Ограничение/смысл |
|---|---:|---|
| `APP_ENV` | `development` | В `production` включает fail-closed проверку internal token |
| `INTERNAL_API_TOKEN` | — | Bearer token для `/internal/*`; в production минимум 32 символа |
| `HTTP_ADDR` | `:8082` | Listen address |
| `PROVIDER_MODE` | `yandex` | `yandex`, `2gis`, or explicit `stub` |
| `ADDRESS_PROVIDER_MODE` | `auto` | In `2gis` route mode, prefer the point-bearing Yandex HTTP Geocoder when its key is configured; otherwise use 2GIS. Explicit values: `yandex`, `2gis` |
| `YANDEX_ROUTER_API_KEY` | — | Обязателен в `yandex`; server secret (`YANDEX_API_KEY` — совместимый alias) |
| `YANDEX_ROUTER_BASE_URL` | official fixed host | Принимается только точное `https://api.routing.yandex.net`; destination не переопределяется |
| `YANDEX_GEOCODER_API_KEY` | router key | Отдельно лицензированный server secret предпочтителен; fallback требует Geocoder entitlement у router key |
| `YANDEX_GEOCODER_BASE_URL` | official fixed host | Принимается только точное `https://geocode-maps.yandex.ru`; destination не переопределяется |
| `YANDEX_MAX_RESULTS` | `3` | `1..3`, не превышает official API |
| `DGIS_API_KEY` | — | Required server-side secret in `2gis` mode; never expose with `NEXT_PUBLIC_` |
| `DGIS_ROUTING_BASE_URL` | official fixed host | Only exact `https://routing.api.2gis.com` is accepted |
| `DGIS_MAX_RESULTS` | `3` | Internal `1..3` cap, including the main route |
| `DGIS_RATE_LIMIT_PER_MINUTE` | `5` | Rolling pre-egress route gate; align with Platform Manager |
| `DGIS_MONTHLY_LIMIT` | `1000` | Surfaced operational quota; Platform Manager is the cross-replica hard limit |
| `DGIS_GEOCODER_RATE_LIMIT_PER_MINUTE` | `600` | Rolling Geocoder guardrail from the current product limit |
| `DGIS_GEOCODER_LOCATION_BIAS` | `37.617635,55.755814` | Official `location=lon,lat` relevance hint for the Moscow product; it does not strictly filter explicitly named cities |
| `PROVIDER_CONNECT_TIMEOUT` | `500ms` | TCP/TLS connect bound |
| `PROVIDER_REQUEST_TIMEOUT` | `2500ms` | На один outbound attempt |
| `PROVIDER_RESPONSE_HEADER_TIMEOUT` | `2s` | Header bound |
| `PROVIDER_MAX_RETRIES` | `2` | `0..5`; дополнительно ограничен request budget/deadline |
| `PROVIDER_RETRY_BASE_DELAY` | `100ms` | Exponential backoff base |
| `PROVIDER_RETRY_MAX_DELAY` | `1s` | Backoff cap; explicit `Retry-After` соблюдается до caller deadline |
| `PROVIDER_MAX_CONCURRENCY` | `32` | Bulkhead capacity |
| `PROVIDER_BULKHEAD_WAIT_TIMEOUT` | `100ms` | Ожидание slot; `0` = immediate reject |
| `PROVIDER_CB_FAILURE_THRESHOLD` | `5` | Retryable failures до open |
| `PROVIDER_CB_OPEN_DURATION` | `30s` | Open interval до half-open probe |
| `PROVIDER_SHUTDOWN_TIMEOUT` | `10s` | Graceful shutdown |
| `PROVIDER_MAX_REQUEST_BODY_BYTES` | `131072` | Internal HTTP body limit |
| `PROVIDER_MAX_RESPONSE_BYTES` | `4194304` | Upstream body limit |
| `PROVIDER_COST_PER_BILLABLE_UNIT` | `0` | Contract-specific cost estimate per billed route response; currency is a deployment convention |
| `PROVIDER_DATA_STORAGE_ALLOWED` | `false` | Capability gate; adapter всё равно не хранит raw payload |
| `PROVIDER_DATA_MODIFICATION_ALLOWED` | `false` | Только после legal/contract confirmation; требует storage=true |
| `ALLOW_EXPERIMENTAL_PROVIDER_SOURCES` | `false` | Inert extension point; scraping не реализован |
| `PROVIDER_STUB_SCENARIO` | `normal` | `normal`, `slow`, `rate_limit`, `outage` |
| `PROVIDER_STUB_DELAY` | `4s` | Context-cancellable delay для `slow` |

## Проверка и сборка

```powershell
gofmt -w services/provider-yandex
go test ./services/provider-yandex
go vet ./services/provider-yandex
go build ./services/provider-yandex
docker build -f services/provider-yandex/Dockerfile -t greenroute/provider-yandex:dev .
```

Distroless/scratch-compatible probe встроен в тот же binary:

```powershell
provider-yandex healthcheck --url http://127.0.0.1:8082/health/ready
```

Contract fixture синтетический и находится в
`tests/contract/provider-yandex/`; реальные ответы провайдера не записываются.
