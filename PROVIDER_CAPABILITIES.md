# Capability spike: routing providers

## 2GIS Routing API adapter (verified 2026-08-05)

`PROVIDER_MODE=2gis` uses only the documented cloud endpoint
`POST https://routing.api.2gis.com/routing/7.0.0/global`. The adapter sends up
to ten route points, requests detailed WKT geometry, supports up to two
alternatives, current (`traffic_mode=jam`) or statistical
(`traffic_mode=statistics`) traffic, documented road filters, hard polygon
exclusions, and toll payment information. One point set is reported as one
estimated billable unit; alternatives in that response are not counted again.
The active Platform Manager subscription is authoritative.

The documented response does not provide an official per-segment congestion
level. Geometry `color` values are intentionally ignored and every normalized
segment remains `congestionClass=UNKNOWN`; current/statistical traffic is only
reported as the route-level data type. A traffic-disabled comparison uses the
documented shortest-distance mode, and the orchestrator must validate geometry
similarity before deriving any delay score.

The current demo guardrails are `DGIS_RATE_LIMIT_PER_MINUTE=5` and
`DGIS_MONTHLY_LIMIT=1000`. The adapter enforces the minute value with a rolling
pre-egress gate and returns `Retry-After` without spending request budget when
full. Platform Manager remains the cross-replica monthly hard limit. Address
lookup uses the official coordinate-bearing
`GET https://catalog.api.2gis.com/3.0/items/geocode`; it is exposed as address
search, not misrepresented as the separate 2GIS Suggest API.

With `ADDRESS_PROVIDER_MODE=auto`, a configured `YANDEX_GEOCODER_API_KEY`
selects the separately licensed point-bearing Yandex HTTP Geocoder while route
calculation remains on 2GIS. On a Yandex error or empty result, the adapter
checks 2GIS Geocoder 3.0; cancellation is propagated immediately and each
provider's quota gate remains authoritative. If the fallback itself fails after
an empty successful Yandex response, the endpoint preserves that empty success
instead of inventing a service outage. Without a Yandex key, 2GIS Geocoder 3.0
is the primary resolver.

The capability response exposes `addressSearchProvider` and
`addressSearchEndpoint`, so this composite is not mislabeled as a single
provider. It also publishes a bounded, privacy-safe `apiIntegrations` array for
operations UI: exact provider, product, API version, capability, primary/fallback
role, and active/standby state. No key, endpoint query, account identifier, or
credential-derived status is included. In the current composite this is 2GIS
Routing 7.0.0, Yandex HTTP Geocoder v1, and 2GIS Geocoder 3.0. The separate
Yandex Geosuggest product is not reported as active because the server does not
call it.

Primary sources:

- https://docs.2gis.com/en/api/navigation/routing/overview
- https://docs.2gis.com/en/api/navigation/routing/reference/routing
- https://docs.2gis.com/en/api/navigation/routing/examples/routing
- https://docs.2gis.com/api/search/geocoder/reference/3.0/items/geocode
- https://docs.2gis.com/platform-manager/subscription/pricing

## Yandex Maps API adapter

Дата проверки: **2026-08-05**. Источники: только публичные первичные документы
Яндекса. Этот файл — техническая фиксация возможностей, а не юридическое
заключение; подписанный договор и настройки конкретного ключа имеют приоритет.

## Вывод

Для production используется официальный HTTP API «Получение деталей маршрута»:
`GET https://api.routing.yandex.net/v2/route`. Он подходит для получения до трёх
автомобильных маршрутов с текущим/прогнозным трафиком либо без учёта пробок,
поддерживает промежуточные точки и avoid-зоны. Документированный ответ **не**
содержит официальный балл пробок или класс загруженности каждого сегмента и не
возвращает одновременно live- и baseline-время одной геометрии. Поэтому
Пробок.Нет выполняет собственное сопоставление live/baseline и до него оставляет
`congestionClass=UNKNOWN`.

## Матрица возможностей

| Возможность | Что гарантирует официальная документация | Решение Пробок.Нет |
|---|---|---|
| Официальный endpoint | `https://api.routing.yandex.net/v2/route`, ключ в `apikey` ([формат запроса](https://yandex.ru/maps-api/docs/router-api/request.html), [quick start](https://yandex.ru/maps-api/docs/router-api/quickstart.html)) | Host, scheme и path зашиты в адаптер; runtime override и redirects запрещены. Ключ только server-side. |
| Альтернативы | `results` от 1 до 3; ответ может содержать меньше; параметр доступен на платных тарифах ([request/results](https://yandex.ru/maps-api/docs/router-api/request.html#results)) | `alternatives=0..2`, общий максимум 3 и дополнительный операторский cap `YANDEX_MAX_RESULTS`. |
| Waypoints | Для `driving`/`truck` максимум 50 элементов, включая начало и конец; для остальных режимов 25 ([request/waypoints](https://yandex.ru/maps-api/docs/router-api/request.html#waypoints)) | Только `driving`; максимум 48 промежуточных точек, то есть 50 всего. |
| Realtime traffic | Без `departure_time` используется ситуация в момент запроса; ответ маркируется `traffic_type=realtime`, `forecast` или `disabled` ([response](https://yandex.ru/maps-api/docs/router-api/response.html#response-params)) | Сохраняем тип в reason code. Не называем собственную оценку официальным уровнем пробок Яндекса. |
| Traffic disabled | `traffic=disabled` строит маршрут по кратчайшему расстоянию без пробок ([request/traffic](https://yandex.ru/maps-api/docs/router-api/request.html#traffic)) | Отдельный baseline-запрос. Геометрия может отличаться от live-маршрута, поэтому требуется geometry matching. |
| Departure time | UNIX time, не в прошлом; без параметра прогноз строится на время обработки. Не применяется при `traffic=disabled` и в отдельных немоторных режимах ([request/departure_time](https://yandex.ru/maps-api/docs/router-api/request.html#departure-time)) | Передаём только для traffic-enabled driving-запроса; прошлое время отклоняется до вызова провайдера. |
| Avoid zones | Для `driving`/`truck` можно передать несколько полигонов, минимум 3 точки в каждом ([request/avoid_zones](https://yandex.ru/maps-api/docs/router-api/request.html#avoid-zones)) | Поддержано. Максимум полигонов/точек у провайдера не опубликован: **UNKNOWN**; внутренний защитный предел — 8 × 32. |
| Платные дороги | `avoid_tolls=true` для `driving`/`truck`, default `false` ([request/avoid_tolls](https://yandex.ru/maps-api/docs/router-api/request.html#avoid-tolls)) | Поддержано. В ответе читаются `hasTolls` и `hasNonTransactionalTolls`. |
| Грунтовые дороги | `avoid_unpaved=true`; доступ указан только для платных тарифов ([request/avoid_unpaved](https://yandex.ru/maps-api/docs/router-api/request.html#avoid-unpaved)) | Поддержано, но фактическое право ключа contract-dependent. |
| География | Продукт явно перечисляет Россию, Абхазию, Турцию, Азербайджан, Армению, Казахстан, Кыргызстан, Таджикистан, Беларусь, Грузию, Узбекистан и Молдову ([страница продукта](https://yandex.ru/maps-api/products/router-api)) | Это консервативный allowlist для UI/документации. Доступность конкретного маршрута проверяется ответом API; покрытие вне списка — **UNKNOWN/contract-dependent**. |
| Технический rate limit | 50 запросов/с; 50 элементов waypoints ([overview/limits](https://yandex.ru/maps-api/docs/router-api/index.html#api-request-limits)) | Bulkhead + bounded retry; локальный QPS limiter не заменяет лимит договора/ключа. |
| Суточный лимит | Единого числа нет. Тарифы задают максимум ответов/сутки: trial 100 на 7 суток; годовые уровни 1 000…1 000 000; выше — по запросу; месячные публично указаны для 1 000 и 10 000 Standard ([официальные тарифы](https://yandex.ru/dev/tariffs/doc/ru/router/prices/)) | **Contract-dependent**. Orchestrator ограничивает каждый поиск `requestBudget`; биллинг/суточную квоту контролирует отдельный cost guard. |
| Единица тарификации | Один вызов функции — Запрос; при нескольких Ответах на один Запрос оплачивается каждый Ответ ([определения](https://yandex.ru/dev/tariffs/doc/ru/router/terms/)) | Отдельно выдаём `requestsUsed` (outbound attempts) и `estimatedBillableUnits` (число маршрутов успешного ответа). Статистика Яндекса/договор окончательны. |
| Ошибки | 400, 401, 429, 500, 504; для 500/504 документация предлагает повтор позже ([ошибки](https://yandex.ru/maps-api/docs/router-api/response.html#error-messages)) | 400/401 не retry; 429 retry с `Retry-After`, если он присутствует; 500/504 и network errors — bounded exponential backoff с full jitter и бюджетом. |
| Ответ | Legs → steps; у step есть `length`, `duration`, `mode`, `polyline`; у route — toll flags ([response schema](https://yandex.ru/maps-api/docs/router-api/response.html#response-params)) | Provider DTO остаётся внутри адаптера; наружу выходит только provider-neutral domain model. |
| Официальный congestion score | В документированной схеме нет jam level, speed, delay или baseline для каждого step | **UNSUPPORTED**. Всегда `UNKNOWN` до собственного парного сравнения. |
| Provider route reference | В документированной схеме стабильный идентификатор маршрута не описан | **UNSUPPORTED/UNKNOWN**; не выдумываем и не сохраняем. |
| Адресный поиск | HTTP Geocoder принимает `geocode`, `lang`, `results`, `format=json`; `GeoObject.Point.pos` содержит долготу и широту ([request](https://yandex.com/maps-api/docs/geocoder-api/request.html), [response](https://yandex.com/maps-api/docs/geocoder-api/response.html)) | `/internal/v1/geosuggest` использует официальный Geocoder, нормализует порядок `lon lat` в `latitude/longitude` и не вызывает Suggest API. Отдельный ключ/пакет contract-dependent. |

## Хранение и лицензии: Standard vs Extended

В актуальных русскоязычных документах используются названия **Стандартная** и
**Расширенная**, а не Basic/Advanced. Поэтому соответствие «Basic = Standard» и
«Advanced = Extended» — только продуктовая нормализация Пробок.Нет, не название
Яндекса.

- Для API деталей маршрута бесплатной production-версии нет
  ([коммерческое описание](https://yandex.ru/dev/commercial/doc/ru/concepts/distance_matrix)).
- Standard разрешает временно кэшировать расстояние, время и детали маршрута до
  30 дней исключительно для повышения производительности лицензированного
  ресурса ([условия, п. 2.1.1](https://yandex.ru/dev/tariffs/doc/ru/router/terms/)).
- Extended разрешает хранить ответ в течение срока действия оферты
  ([условия, п. 2.1.2](https://yandex.ru/dev/tariffs/doc/ru/router/terms/)).
- Публичное коммерческое описание говорит, что Extended снимает запрет на
  сохранение и изменение, но те же специальные условия содержат общее
  обязательство не модифицировать данные. Поэтому право **изменять** конкретные
  данные помечено `UNKNOWN/contract-dependent` до письменного подтверждения.
- Реализация по умолчанию использует `PROVIDER_DATA_STORAGE_ALLOWED=false` и
  `PROVIDER_DATA_MODIFICATION_ALLOWED=false`; сырые ответы не пишутся ни в БД,
  ни в кэш, ни в логи. Даже при разрешении storage текущий адаптер raw payload не
  сохраняет.

## Ключи и безопасность

Ключи передаются только между `provider-yandex` и фиксированными официальными
hosts Router/Geocoder.
Документация позволяет ограничить ключ IP-адресами/доменами; для server-to-server
ключа следует настроить allowlist egress IP
([ограничения ключа](https://yandex.ru/maps-api/docs/router-api/limit.html)).
Процесс не принимает URL провайдера из запроса или environment, игнорирует
ambient proxy variables и запрещает redirects, исключая SSRF/утечку ключа через
динамический destination.

## Адресный поиск: Geocoder вместо Suggest

Официальный Geosuggest — отдельный продукт и отдельный пакет ключа. Его ответ
возвращает title/subtitle и `uri`, но не координаты
([request](https://yandex.ru/maps-api/docs/suggest-api/request.html),
[response](https://yandex.ru/maps-api/docs/suggest-api/response.html)). Поскольку
provider-neutral `GeoSuggestion` требует `GeoPoint`, production endpoint
`/internal/v1/geosuggest` реализован через отдельно лицензированный официальный
[HTTP Geocoder](https://yandex.com/maps-api/docs/geocoder-api/request.html), а не
через Suggest. Из `GeoObject.Point.pos` адаптер читает документированный порядок
`longitude latitude` и нормализует его в `GeoPoint`.

Предпочтителен отдельный secret `YANDEX_GEOCODER_API_KEY`. При его отсутствии
используется `YANDEX_ROUTER_API_KEY`, но такой fallback работает только если этот
же ключ подключён к пакету Geocoder. Право использования, квота и стоимость
Geocoder зависят от договора; опубликованный лимит Router 50 RPS на Geocoder не
переносится. Ответы проходят тот же timeout/bulkhead/retry/circuit-breaker
boundary, не логируются и не сохраняются. Условия Geocoder отдельно описывают
кэширование до 30 дней для Standard и хранение на срок лицензии для Extended
([условия](https://yandex.ru/dev/tariffs/doc/ru/geocoder/terms/)); runtime default
остаётся no-store. Stub предоставляет детерминированные синтетические точки.

## Неизвестные и contract-dependent параметры

- точная доступность маршрутизации на границе/вне публичного списка стран;
- фактическая суточная квота, overage и цена конкретного ключа;
- максимальное число avoid-полигонов и точек полигона;
- SLA, Retry-After для каждого 429 и серверные burst-правила;
- квота, RPS, стоимость и entitlement конкретного ключа HTTP Geocoder;
- право модификации/производного хранения данных по конкретному договору;
- доступность `results`, `avoid_unpaved` и иных paid features у конкретного ключа;
- семантика стабильности маршрута между live и baseline запросами.

Все эти значения в runtime считаются **UNKNOWN** или **contract-dependent**, а не
заменяются оптимистичными значениями.
