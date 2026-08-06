# ADR-002: Изоляция картографического провайдера

- Статус: принято
- Дата: 2026-08-05

## Контекст

Scoring и frontend не должны зависеть от DTO, ключей, ошибок и лицензионных
особенностей Яндекса. Одновременно нам нужны production-адаптер официального API,
детерминированный локальный stub, единый request budget и возможность заменить
провайдера без переписывания доменной логики.

## Решение

Выделить `provider-yandex` в отдельный stateless HTTP-сервис и сделать его
единственной точкой server-to-server доступа к официальным API Яндекса.

Внутренний контракт:

- `POST /internal/v1/routes` — provider-neutral request/response из
  `internal/contracts` и `internal/domain`;
- `GET /internal/v1/capabilities` — фактические возможности активного adapter;
- `GET /internal/v1/geosuggest` — provider-neutral адресный поиск: в real-mode
  официальный HTTP Geocoder, в stub детерминированные синтетические точки;
- `/health/live`, `/health/ready`, `/metrics` — эксплуатационный контракт.

Yandex wire DTO не экспортируются из пакета адаптера. Ключ добавляется только в
исходящий запрос к фиксированным `https://api.routing.yandex.net/v2/route` и
`https://geocode-maps.yandex.ru/v1/`.
Переопределить host из HTTP-запроса или environment нельзя; redirects и ambient
proxy запрещены. Сырые тела ответов ограничены размером, только декодируются в
памяти и никогда не логируются/сохраняются.

Resilience находится в adapter boundary: connect/request/header timeout,
concurrency bulkhead, circuit breaker, bounded retries с jitter, `Retry-After` для
429 и расходование `requestBudget` перед каждым outbound attempt. HTTP attempts и
оценочные billable response units учитываются отдельно.

Production default — `PROVIDER_MODE=yandex` с fail-closed требованием
`YANDEX_ROUTER_API_KEY` (`YANDEX_API_KEY` — совместимый alias). Для адресов
предпочтителен отдельно лицензированный `YANDEX_GEOCODER_API_KEY`; fallback на
router key допустим только при Geocoder entitlement. `PROVIDER_MODE=stub`
включается явно и помечает каждый ответ как synthetic.
`ALLOW_EXPERIMENTAL_PROVIDER_SOURCES` является inert extension point:
даже `true` не включает scraping или undocumented endpoint.

## Последствия

Плюсы:

- ключ и provider-specific schema изолированы;
- scoring тестируется на стабильном stub;
- ошибки и метрики не раскрывают URL, payload, ключ или координаты;
- новый официальный провайдер реализует тот же внутренний контракт.

Цена решения — отдельный сетевой hop, явное преобразование моделей и отдельный
Geocoder package для адресного поиска. Возможность, которой нет в официальном
контракте, остаётся unsupported вместо заполнения фиктивными данными.

## Отклонённые варианты

- Передавать Yandex DTO напрямую в orchestrator: создаёт vendor lock-in и утечку
  лицензионной семантики во все сервисы.
- Вызывать Router API из browser: раскрывает server key и лишает нас единого
  budget/resilience boundary.
- Использовать JavaScript API как backend: это иной client-side продукт и иной
  контракт.
- Использовать scraping/undocumented traffic endpoints: хрупко, не проходит
  production security/compliance; dev-флаг оставлен только как extension point.

Официальные основания: [request](https://yandex.ru/maps-api/docs/router-api/request.html),
[response](https://yandex.ru/maps-api/docs/router-api/response.html),
[ограничения ключа](https://yandex.ru/maps-api/docs/router-api/limit.html),
[Geocoder request](https://yandex.com/maps-api/docs/geocoder-api/request.html),
[Geocoder response](https://yandex.com/maps-api/docs/geocoder-api/response.html).
