# Риски и неизвестные

| Риск | Влияние | Контроль / условие закрытия |
|---|---|---|
| Тариф и права хранения зависят от конкретного договора Yandex | стоимость или нарушение лицензии | billable units отдельно от money; storage/cache off; legal checklist перед production |
| Реальный Router/Geocoder не проверен без ключей проекта | контрактная ошибка проявится только в smoke | contract tests синтетические; провести credentialed staging smoke и записать дату |
| Traffic-disabled маршрут может выбрать другой коридор | ложная оценка congestion | forced control points + geometry similarity; mismatch → UNKNOWN/low confidence |
| Provider отвечает максимум тремя route results | мало разнообразия | bounded enhanced search via official avoid zones/anchors; explicit exhaustion reason |
| In-memory orchestrator state при storage denied | одна реплика и потеря active state при restart | short searches + client retry; Redis только после license approval |
| Геокодер и Router могут иметь разные условия/coverage | address найден, route недоступен | capability/runbook, user-facing normalized error, staging coverage tests |
| Реальный денежный тариф неизвестен runtime | estimated cost невозможно подтвердить | возвращать units; money remains 0 до operator-configured contract tariff |
| OIDC claim shape отличается у IdP | admin access fail closed | staging claim-mapping test; configurable admin group; no implicit admin |
| Enhanced detour может ухудшить безопасность/удобство | нежелательный объезд | bounded zones/iterations/area, hard distance/time limits, material improvement threshold |
| Live rerouting отвлекает водителя | UX/safety risk | feature off by default, hysteresis and explicit user confirmation before rollout |
| SLO не подтверждены реальной нагрузкой/квотой | capacity uncertainty | k6 profiles + provider sandbox/staging load within contract; revise SLO after evidence |

## Осознанно неподдерживаемое по умолчанию

`ALLOW_EXPERIMENTAL_PROVIDER_SOURCES` не активирует scraping автоматически: extension point существует, но production adapter остаётся официальным. Любой экспериментальный источник должен быть отдельным adapter mode, иметь kill switch, не смешиваться с официальными claims и пройти юридическую/security проверку.
