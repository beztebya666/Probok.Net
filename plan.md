
Ты — principal software architect, staff backend engineer, senior frontend engineer, SRE и security engineer в одном лице.

Требуется спроектировать и реализовать production-ready web-приложение для построения автомобильных маршрутов, приоритетом которого является не минимальное расстояние и не минимальный ETA, а минимальное количество пробок и максимально равномерное движение.

Рабочее название проекта: GreenRoute.

# 1. Цель продукта

Пользователь задаёт:

* начальную точку;
* конечную точку;
* необязательные промежуточные точки;
* максимально допустимый дополнительный километраж;
* максимально допустимое дополнительное время;
* желаемый режим маршрутизации.

Поддержать режимы:

1. FASTEST — самый быстрый маршрут.
2. BALANCED — компромисс между временем, расстоянием и пробками.
3. GREENEST — максимальное избегание загруженных участков.
4. STRICT_GREEN — минимизировать красные участки даже ценой существенного объезда, но оставаться в заданных пользователем ограничениях.

Пример пользовательского требования:

«Разрешить маршрут на 30 км длиннее и на 20 минут дольше, если он практически полностью проходит по свободным дорогам».

Продукт не должен утверждать, что дорога свободна, если имеющихся данных недостаточно. Каждый результат должен иметь показатель confidence и понятное объяснение выбора.

# 2. Критические ограничения

Использовать только официально документированные API картографического провайдера.

Основной провайдер первой версии — Yandex Maps API:

* Yandex Router API для серверного построения маршрутов;
* Yandex JavaScript API для отображения карты;
* официальный Geocoder или Geosuggest для поиска адресов.

Запрещено:

* парсить сайт Яндекс Карт;
* извлекать данные из undocumented endpoints;
* перехватывать внутренние запросы приложения;
* скрейпить traffic tiles;
* использовать неофициальные API;
* передавать серверный API-ключ в браузер;
* называть собственную оценку загруженности официальным уровнем пробок Яндекса.

Перед реализацией выполнить provider capability spike и зафиксировать в ADR:

* доступное количество альтернативных маршрутов;
* максимальное количество waypoints;
* поддержку realtime traffic;
* поддержку traffic disabled;
* поддержку departure time;
* поддержку avoid zones;
* доступную географию;
* лимиты запросов;
* разрешённые условия кэширования и хранения данных;
* различия Basic и Advanced лицензий.

Если фактическая документация расходится с предположениями из этого задания, документация провайдера является источником истины. Расхождение оформить отдельным ADR и скорректировать реализацию без скрытого обхода ограничений.

# 3. Архитектурные принципы

Создать микросервисную архитектуру без искусственного дробления и distributed monolith.

Минимальный состав:

## 3.1 web-app

Frontend на TypeScript и React с подходящим production-фреймворком.

Функции:

* карта;
* ввод начальной и конечной точки;
* geosuggest;
* отображение альтернатив;
* управление ограничениями;
* просмотр параметров маршрута;
* отображение статуса фонового поиска;
* SSE-подписка на ход расширенного поиска;
* адаптивная mobile-first вёрстка;
* PWA-режим;
* доступность WCAG AA;
* русский и английский языки.

## 3.2 edge-api

Публичный Backend for Frontend/API Gateway.

Ответственность:

* REST API;
* аутентификация;
* авторизация;
* rate limiting;
* idempotency;
* валидация;
* нормализация ошибок;
* correlation ID;
* запуск поиска маршрута;
* SSE endpoint;
* защита внутренних сервисов;
* OpenAPI 3.1.

## 3.3 routing-orchestrator

Оркестрация поиска маршрутов.

Ответственность:

* получение исходных кандидатов;
* запуск оценки;
* генерация дополнительных коридоров;
* формирование avoid zones;
* контроль бюджета provider requests;
* дедупликация маршрутов;
* остановка поиска;
* выбор победителя;
* graceful degradation.

## 3.4 provider-yandex

Изолированный адаптер Yandex API.

Ответственность:

* формирование запросов;
* управление ключами;
* timeouts;
* retries с jitter;
* circuit breaker;
* обработка 429, 500 и 504;
* преобразование ответа Яндекса во внутреннюю модель;
* capability checks;
* учёт стоимости запросов;
* запрет утечки provider-specific DTO в остальные сервисы.

Внутренний интерфейс провайдера должен позволять подключить в дальнейшем другого поставщика без изменения scoring engine и frontend.

## 3.5 route-scoring

Детерминированный сервис или изолированный доменный модуль оценки маршрутов.

Он не должен обращаться к картографическому API самостоятельно.

Ответственность:

* сравнение live и baseline маршрутов;
* классификация участков;
* расчёт метрик;
* ранжирование;
* confidence score;
* reason codes;
* объяснение результата.

Если нагрузочное тестирование не докажет необходимость отдельного deployment, route-scoring допускается оставить отдельным модулем routing-orchestrator. Граница домена и контракты при этом должны сохраниться.

# 4. Рекомендуемый технологический стек

Backend:

* Go;
* REST/JSON для публичного API;
* gRPC или ConnectRPC для внутренних контрактов;
* PostgreSQL для пользовательских данных и аудита;
* Redis только для rate limiting, locks, idempotency и разрешённого лицензией краткосрочного кэша;
* structured logging;
* OpenTelemetry.

Frontend:

* TypeScript;
* React;
* production framework с SSR там, где это уместно;
* TanStack Query или аналогичный query layer;
* runtime validation входящих API-ответов;
* компонентный UI;
* Playwright.

Infrastructure:

* Docker;
* Kubernetes;
* Helm;
* GitOps-compatible manifests;
* PostgreSQL;
* Redis;
* Prometheus-compatible metrics;
* Grafana dashboards;
* OpenTelemetry Collector;
* tracing backend;
* centralized logs.

Использовать актуальные стабильные версии зависимостей на момент реализации. Все версии зафиксировать lock-файлами, toolchain-файлами и container digests. Не использовать `latest`.

# 5. Внутренняя доменная модель

Создать provider-neutral модели:

RouteSearchRequest:

* requestId;
* origin;
* destination;
* waypoints;
* departureTime;
* routingMode;
* maxExtraDistanceMeters;
* maxExtraDistancePercent;
* maxExtraTimeSeconds;
* avoidTolls;
* avoidUnpaved;
* strictness;
* maxProviderRequests;
* searchDeadlineMs.

GeoPoint:

* latitude;
* longitude.

RouteCandidate:

* candidateId;
* provider;
* providerRouteReference, если разрешено;
* geometry;
* distanceMeters;
* liveDurationSeconds;
* baselineDurationSeconds;
* trafficDelaySeconds;
* segments;
* blocked;
* tolls;
* confidence;
* score;
* reasonCodes;
* generatedBy;
* providerRequestCount.

RouteSegment:

* segmentId;
* geometry;
* distanceMeters;
* liveDurationSeconds;
* baselineDurationSeconds;
* trafficRatio;
* congestionClass;
* confidence;
* geometrySimilarity;
* source.

CongestionClass:

* GREEN;
* YELLOW;
* ORANGE;
* RED;
* UNKNOWN.

RouteSearchResult:

* searchId;
* status;
* selectedRoute;
* alternatives;
* fastestReferenceRoute;
* constraints;
* providerUsage;
* warnings;
* generatedAt;
* expiresAt.

Никогда не подменять UNKNOWN значением GREEN.

# 6. Базовый поиск кандидатов

Алгоритм первого этапа:

1. Провалидировать координаты и ограничения.
2. Получить у провайдера максимально доступное количество альтернатив с учётом текущих пробок.
3. Исключить маршруты с ошибками или закрытыми участками.
4. Нормализовать геометрию.
5. Дедуплицировать практически одинаковые маршруты.
6. Определить самый быстрый маршрут как reference route.
7. Передать кандидаты на baseline evaluation.

Для дедупликации использовать сочетание:

* доли пространственного перекрытия;
* Hausdorff или Fréchet distance;
* сходства длины;
* сходства ключевых коридоров.

Порог вынести в конфигурацию.

# 7. Оценка загруженности

Поскольку провайдер может не возвращать официальный congestion level для каждого дорожного участка, реализовать собственную оценку.

Для каждого кандидата:

1. Упростить polyline без потери ключевых поворотов.
2. Разбить маршрут на контрольные точки.
3. Сохранить начало, конец, крупные повороты и смены дорожных коридоров.
4. Не превышать provider waypoint limit.
5. Построить маршрут через те же контрольные точки:

   * с текущими пробками;
   * с traffic disabled или аналогичным baseline-режимом.
6. Проверить, что live и baseline геометрии действительно проходят по сопоставимому коридору.
7. Если geometry similarity ниже порога, пометить участок UNKNOWN и снизить confidence.
8. Сопоставить участки по геометрии.
9. Рассчитать trafficRatio.

Базовые настраиваемые пороги:

* GREEN: ratio < 1.15;
* YELLOW: 1.15 <= ratio < 1.35;
* ORANGE: 1.35 <= ratio < 1.65;
* RED: ratio >= 1.65.

Это эвристика приложения, а не официальная классификация поставщика.

Рассчитать:

* greenDistanceMeters;
* yellowDistanceMeters;
* orangeDistanceMeters;
* redDistanceMeters;
* greenDurationSeconds;
* yellowDurationSeconds;
* orangeDurationSeconds;
* redDurationSeconds;
* totalTrafficDelaySeconds;
* congestedDistancePercent;
* congestedDurationPercent;
* worstContinuousCongestionMeters;
* routeConfidence.

Не рассчитывать фиктивный stop count, если провайдер не предоставляет данных для его определения.

# 8. Генерация новых маршрутов

Если исходные кандидаты не удовлетворяют режиму GREENEST или STRICT_GREEN:

1. Найти непрерывные кластеры RED и ORANGE.
2. Отбросить кластеры слишком близко к origin или destination, если объехать их физически невозможно.
3. Построить небольшие buffered polygons вокруг наиболее тяжёлых кластеров.
4. Отправить новые запросы с avoid zones.
5. Дополнительно сгенерировать ограниченное число corridor anchors:

   * севернее проблемного участка;
   * южнее;
   * восточнее;
   * западнее;
   * по крупным обходным дорогам, если они доступны через документированный API.
6. Получить новые маршруты через via points.
7. Провести дедупликацию.
8. Оценить новые маршруты тем же способом.

Запрещено создавать гигантские avoid zones, которые могут блокировать целый город или вынуждать опасный маршрут.

Ввести ограничения:

* не более заданного числа provider requests;
* максимальное wall-clock время поиска;
* максимальное количество итераций;
* максимальное количество active candidates;
* максимальное число avoid polygons;
* максимальную площадь polygon;
* минимальное улучшение score для продолжения поиска.

По умолчанию один пользовательский поиск не должен бесконтрольно порождать десятки платных API-запросов.

# 9. Ранжирование

Не использовать только одну непрозрачную сумму весов для режима STRICT_GREEN.

Сначала применить hard constraints:

* маршрут не заблокирован;
* дополнительное расстояние не превышает пользовательский лимит;
* дополнительное время не превышает лимит, если пользователь его установил;
* confidence не ниже минимального допустимого значения;
* геометрия валидна.

Для STRICT_GREEN использовать лексикографическое сравнение:

1. redDurationSeconds;
2. redDistanceMeters;
3. orangeDurationSeconds;
4. totalTrafficDelaySeconds;
5. worstContinuousCongestionMeters;
6. liveDurationSeconds;
7. distanceMeters.

Для GREENEST использовать похожий порядок, но разрешить configurable penalty для дополнительного времени.

Для BALANCED использовать документированную нормализованную функцию:

score =
trafficPenalty

* etaPenalty
* distancePenalty
* uncertaintyPenalty
* tollPenalty.

Все веса:

* хранить в конфигурации;
* версионировать;
* возвращать в debug/admin response;
* покрыть golden tests;
* не изменять без отдельной версии scoring policy.

Результат должен содержать reason codes, например:

* LOWEST_RED_DURATION;
* NO_RED_SEGMENTS;
* LONGER_BUT_SMOOTHER;
* WITHIN_EXTRA_DISTANCE_LIMIT;
* LOW_CONFIDENCE_BASELINE;
* PROVIDER_ALTERNATIVES_EXHAUSTED;
* SEARCH_BUDGET_EXHAUSTED;
* NO_GREEN_ROUTE_AVAILABLE.

# 10. Confidence model

Рассчитать confidence на основании:

* типа traffic data;
* свежести данных;
* сходства live и baseline геометрии;
* полноты сопоставления сегментов;
* количества UNKNOWN участков;
* длины интервала между контрольными точками;
* наличия альтернатив;
* provider errors;
* времени, прошедшего с расчёта.

Уровни:

* HIGH;
* MEDIUM;
* LOW.

При LOW confidence интерфейс должен показывать:

«Свободность маршрута оценена приблизительно: поставщик не предоставил достаточно детальных данных для части участков».

# 11. Публичный API

Реализовать:

POST /api/v1/route-searches

Поддержать Idempotency-Key.

Ответ:

* 200, если быстрый расчёт завершён;
* 202, если расширенный поиск продолжается.

GET /api/v1/route-searches/{searchId}

GET /api/v1/route-searches/{searchId}/events

SSE events:

* SEARCH_ACCEPTED;
* PROVIDER_REQUEST_STARTED;
* INITIAL_CANDIDATES_READY;
* CANDIDATE_EVALUATED;
* DETOUR_SEARCH_STARTED;
* BETTER_ROUTE_FOUND;
* SEARCH_COMPLETED;
* SEARCH_DEGRADED;
* SEARCH_FAILED.

DELETE /api/v1/route-searches/{searchId}

Удаляет пользовательский search state в рамках разрешённой модели хранения.

Добавить:

GET /health/live
GET /health/ready
GET /metrics

Создать строгую OpenAPI 3.1 спецификацию и сгенерированные клиенты.

# 12. Интерфейс пользователя

Главный экран:

* полноэкранная карта;
* поле «Откуда»;
* поле «Куда»;
* кнопка определения текущего положения;
* режим маршрута;
* ползунок «Допустимый объезд, км»;
* ограничение дополнительного времени;
* переключатели платных и грунтовых дорог;
* кнопка «Найти свободный маршрут».

Карточка каждого маршрута:

* ETA;
* расстояние;
* дополнительное время относительно самого быстрого;
* дополнительное расстояние;
* оценочное время в красной зоне;
* оценочное время в оранжевой зоне;
* процент свободного пути;
* confidence;
* объяснение выбора.

Выбранный маршрут должен быть визуально выделен.

Сегменты раскрашивать согласно собственной модели, но в легенде явно написать:

«Цвет маршрута — оценка GreenRoute, а не официальный цвет пробок картографического сервиса».

Предоставить режим сравнения:

* самый быстрый;
* самый зелёный;
* рекомендуемый.

Пример объяснения:

«Маршрут на 18,4 км длиннее и на 11 минут дольше, но сокращает оценочное время в тяжёлой пробке с 24 до 3 минут».

Не использовать dark patterns и не скрывать дополнительный километраж.

# 13. Re-routing

Поддержать обновление маршрута, пока приложение открыто.

Условия перестроения:

* пользователь ушёл с маршрута;
* маршрут устарел;
* существенно изменился traffic score;
* появился маршрут, который улучшает redDuration не менее чем на заданный порог;
* текущий маршрут стал заблокирован.

Добавить hysteresis:

* не предлагать перестроение из-за незначительного выигрыша;
* не переключаться между двумя маршрутами каждые несколько минут;
* учитывать стоимость и отвлечение пользователя.

Показывать причину:

«Впереди появилась пробка. Новый маршрут длиннее на 7 км, но экономит около 16 минут стояния».

# 14. Хранение данных и лицензирование

Создать конфигурацию:

PROVIDER_DATA_STORAGE_ALLOWED=false
PROVIDER_DATA_MODIFICATION_ALLOWED=false
PROVIDER_RESPONSE_CACHE_TTL=0

По умолчанию:

* не сохранять сырые ответы картографического API;
* не сохранять provider polyline в постоянную БД;
* не создавать долгоживущие response fixtures из реальных ответов;
* не использовать Redis для provider responses;
* не экспортировать provider data в analytics.

Разрешать хранение или кэширование только после явного изменения конфигурации, подтверждённого лицензией и ADR.

Для тестов использовать:

* синтетические fixtures;
* вручную сформированные provider-neutral ответы;
* официально разрешённые тестовые данные.

Пользовательские координаты считать чувствительными данными.

Не писать в логи:

* точные origin и destination;
* полную историю перемещений;
* API-ключи;
* сырые provider responses;
* access tokens.

Для аналитики использовать округлённые или агрегированные значения, когда это допустимо.

# 15. Безопасность

Реализовать:

* OIDC/OAuth 2.1;
* короткоживущие access tokens;
* API key только в server-side secret storage;
* отдельный ограниченный browser key для карты;
* request schema validation;
* rate limiting по пользователю и IP;
* abuse detection;
* CORS allowlist;
* CSP;
* CSRF protection там, где применимо;
* secure cookies;
* SSRF protection;
* output encoding;
* dependency scanning;
* secret scanning;
* SAST;
* container scanning;
* SBOM;
* image signing;
* non-root containers;
* read-only root filesystem;
* dropped Linux capabilities;
* Kubernetes NetworkPolicy;
* Pod Security Standards;
* encrypted transport;
* rotation procedure для provider keys.

Ошибки внешнего провайдера не должны раскрывать пользователю внутренние URL, ключи или сырые payload.

# 16. Надёжность

Для provider client реализовать:

* connect timeout;
* request timeout;
* bounded retry;
* exponential backoff;
* jitter;
* circuit breaker;
* concurrency limit;
* bulkhead;
* корректную обработку 429;
* Retry-After;
* budget-aware retries.

Не повторять запрос при клиентской ошибке 4xx, кроме явно временных случаев.

Graceful degradation:

* при невозможности выполнить расширенный поиск вернуть исходные маршруты;
* явно пометить, что green optimization недоступна;
* не превращать provider outage в бесконечную очередь;
* не выбирать маршрут с неизвестными параметрами как гарантированно свободный.

# 17. Наблюдаемость

Использовать OpenTelemetry end-to-end.

Каждый route search должен иметь:

* traceId;
* searchId;
* requestId;
* provider request spans;
* scoring spans;
* candidate generation spans.

Метрики:

* route_search_total;
* route_search_duration_seconds;
* initial_candidates_duration_seconds;
* enhanced_search_duration_seconds;
* provider_requests_total;
* provider_request_duration_seconds;
* provider_errors_total;
* provider_429_total;
* provider_circuit_breaker_state;
* candidates_generated_total;
* candidates_deduplicated_total;
* green_route_found_total;
* no_green_route_total;
* search_budget_exhausted_total;
* low_confidence_results_total;
* estimated_provider_cost_total;
* selected_route_extra_distance_meters;
* selected_route_red_duration_seconds.

Не добавлять точные координаты в metric labels.

Подготовить Grafana dashboards:

1. Product overview.
2. Provider health and cost.
3. Routing quality.
4. API latency and errors.
5. Low-confidence and degraded searches.

Подготовить alert rules.

# 18. SLO

Задать начальные цели:

* публичный API availability: 99.9%;
* p95 initial route response: менее 3 секунд при нормальной работе провайдера;
* p95 enhanced search completion: менее 10 секунд;
* provider request timeout не должен превышать общий search deadline;
* доля необъяснимых 5xx: менее 0.1%;
* 100% результатов содержат confidence и reason codes;
* 0 утечек server API key в frontend bundle и logs.

SLO должны быть конфигурируемыми и пересматриваться после нагрузочного тестирования.

# 19. Тестирование

Обязательно реализовать:

## Unit tests

* scoring thresholds;
* lexicographic ranking;
* hard constraints;
* confidence;
* geometry matching;
* deduplication;
* provider error mapping;
* request budget;
* stop conditions.

## Golden tests

Создать фиксированный набор синтетических маршрутов:

* короткий маршрут с тяжёлой пробкой;
* длинный полностью свободный;
* длинный с одной красной зоной;
* маршрут с UNKNOWN сегментами;
* маршрут за пределами допустимого объезда;
* одинаковые маршруты от провайдера;
* route geometry mismatch;
* отсутствие свободного варианта.

Scoring должен быть детерминированным.

## Contract tests

Проверять provider adapter относительно официальной схемы.

Не сохранять реальные provider responses, если это не разрешено лицензией.

## Integration tests

* PostgreSQL;
* Redis;
* edge-api;
* orchestrator;
* provider stub;
* SSE;
* idempotency;
* cancellation.

## End-to-end tests

Playwright:

* поиск адресов;
* построение;
* изменение режима;
* изменение допустимого объезда;
* выбор альтернативы;
* degraded provider;
* low confidence;
* mobile viewport.

## Load tests

Проверить:

* пиковый поток route searches;
* медленный provider;
* 429;
* provider outage;
* reconnect SSE;
* массовую отмену запросов;
* отсутствие retry storm.

# 20. CI/CD

Pipeline должен выполнять:

1. formatting;
2. lint;
3. unit tests;
4. contract tests;
5. integration tests;
6. frontend tests;
7. build;
8. SBOM;
9. vulnerability scan;
10. image signing;
11. Helm lint;
12. Kubernetes schema validation;
13. ephemeral environment;
14. end-to-end tests;
15. controlled deployment.

Подготовить:

* Dockerfiles;
* docker-compose для локальной разработки;
* Helm charts;
* values-dev.yaml;
* values-stage.yaml;
* values-prod.yaml;
* HorizontalPodAutoscaler;
* PodDisruptionBudget;
* NetworkPolicy;
* ServiceMonitor;
* alert rules;
* dashboards;
* migration job;
* rollback procedure;
* disaster recovery notes.

# 21. Репозиторий

Создать monorepo:

/apps/web
/services/edge-api
/services/routing-orchestrator
/services/provider-yandex
/internal/contracts
/internal/domain
/internal/scoring
/deploy/helm
/deploy/kubernetes
/observability
/docs/adr
/docs/api
/docs/runbooks
/tests/contract
/tests/e2e
/tests/load
/tools

Добавить:

* README.md;
* ARCHITECTURE.md;
* SECURITY.md;
* PRIVACY.md;
* CONTRIBUTING.md;
* THREAT_MODEL.md;
* PROVIDER_CAPABILITIES.md;
* LICENSE_COMPLIANCE_CHECKLIST.md;
* RUNBOOK_PROVIDER_OUTAGE.md;
* RUNBOOK_PROVIDER_RATE_LIMIT.md;
* RUNBOOK_KEY_ROTATION.md.

# 22. ADR

Минимальный набор:

ADR-001 Service boundaries.
ADR-002 Provider abstraction.
ADR-003 Traffic estimation model.
ADR-004 Route ranking policy.
ADR-005 Provider data retention.
ADR-006 Async search and SSE.
ADR-007 Geometry matching.
ADR-008 Request-budget enforcement.
ADR-009 Geographic coverage strategy.
ADR-010 Security and privacy model.

# 23. Административный интерфейс

Добавить защищённый admin view:

* состояние провайдера;
* количество запросов;
* estimated cost;
* circuit breaker;
* активная scoring policy;
* процент degraded results;
* low-confidence distribution;
* search budget exhaustion;
* feature flags.

Не отображать точные пользовательские маршруты без отдельного законного основания и соответствующего доступа.

# 24. Feature flags

Поддержать:

* ENABLE_ENHANCED_SEARCH;
* ENABLE_AVOID_ZONE_GENERATION;
* ENABLE_CORRIDOR_ANCHORS;
* ENABLE_ROUTE_RERANKING;
* ENABLE_LIVE_REROUTING;
* ENABLE_PROVIDER_CACHE;
* PROVIDER_DATA_STORAGE_ALLOWED;
* PROVIDER_DATA_MODIFICATION_ALLOWED;
* ENABLE_ANONYMOUS_USAGE;
* ENABLE_ADMIN_DEBUG_DETAILS.

Feature flags должны иметь безопасные значения по умолчанию.

# 25. Критерии приёмки

Решение считается принятым, когда:

1. Пользователь может задать origin, destination и допустимый объезд.
2. Сервис получает альтернативы через официальный provider adapter.
3. Для каждого маршрута рассчитывается собственная оценка загруженности.
4. GREENEST предпочитает более свободный маршрут, даже если он длиннее.
5. Ограничение дополнительного километража никогда не нарушается молча.
6. При отсутствии свободного маршрута сервис честно сообщает об этом.
7. Каждый результат содержит confidence и reason codes.
8. UNKNOWN никогда автоматически не считается GREEN.
9. Provider key отсутствует в frontend bundle.
10. Provider outage обрабатывается без каскадного отказа.
11. Стоимость API-запросов ограничена request budget.
12. Сырые provider responses не сохраняются при выключенном разрешении.
13. Scoring покрыт детерминированными golden tests.
14. OpenAPI актуален и проверяется в CI.
15. Kubernetes deployment проходит readiness и graceful shutdown.
16. Метрики, traces, dashboards и alerts работают.
17. Есть документация запуска, эксплуатации и отката.
18. Нет TODO-заглушек в критическом runtime path.
19. Все security-critical настройки имеют fail-closed поведение.
20. Реализация не использует недокументированные API.

# 26. Порядок выполнения

Работать итеративно, но не оставлять проект в состоянии набора несвязанных заготовок.

Этап 1:

* provider capability spike;
* ADR;
* domain model;
* OpenAPI;
* архитектурный skeleton.

Этап 2:

* Yandex adapter;
* provider stub;
* первоначальные кандидаты;
* карта и базовый UI.

Этап 3:

* baseline evaluation;
* scoring;
* confidence;
* golden tests.

Этап 4:

* enhanced detour search;
* avoid zones;
* request budgets;
* SSE.

Этап 5:

* security;
* observability;
* resilience;
* Kubernetes;
* CI/CD.

Этап 6:

* load tests;
* failure tests;
* documentation;
* acceptance report.

После каждого этапа:

* запускать тесты;
* обновлять документацию;
* фиксировать принятые решения;
* не скрывать ограничения;
* не имитировать готовность неработающих функций.

# 27. Формат результата работы coding-agent

Сначала выдать:

1. краткое резюме архитектуры;
2. дерево репозитория;
3. список ADR;
4. provider capability matrix;
5. список рисков и неизвестных;
6. план реализации.

Затем создать рабочий код.

После создания кода выдать:

* инструкции локального запуска;
* команды тестирования;
* команды сборки;
* команды развертывания;
* пример `.env.example` без секретов;
* примеры API-запросов;
* screenshots или описание ключевых экранов;
* результаты тестов;
* known limitations;
* production readiness checklist.

Не ограничиваться концептом, псевдокодом или несколькими демонстрационными файлами. Итогом должен стать запускаемый, тестируемый и документированный репозиторий.



На самом деле в плане есть недоработки в плане запретов типо:

Запрещено:

парсить сайт Яндекс Карт;

извлекать данные из undocumented endpoints;

перехватывать внутренние запросы приложения;

скрейпить traffic tiles;

использовать неофициальные API;

передавать серверный API-ключ в браузер;

называть собственную оценку загруженности официальным уровнем пробок Яндекса.

-- На самом деле разрешено всё, ты можешь просить о помощи когда тебе нужен API-ключ или чето типо того (потому что тут нужен мой ручной труд) - и я тебе его скину, страха за API-ключ нет, потому что пока что мы это поднимаем на localhost'e
