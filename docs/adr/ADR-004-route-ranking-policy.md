# ADR-004: Политика ранжирования маршрутов

- Статус: принято
- Дата: 2026-08-05
- Policy: `greenroute-scoring-v1.0.0`

## Решение

Всегда сначала применять hard constraints: blocked/geometry, absolute и percentage distance, extra time, confidence, toll/unpaved. Нарушившие route не получают штраф — они исключаются с reason.

`STRICT_GREEN` сравнивает лексикографически: red duration, red distance, orange duration, traffic delay, worst continuous congestion, live duration, distance. `GREENEST` использует тот же congestion-first порядок с явным configurable extra-time penalty. `FASTEST` сортирует ETA/distance. `BALANCED` использует документированное произведение нормализованных penalties.

Weights/thresholds хранятся в `configs/scoring-policy-v1.json`; сумма balanced weights равна 1 и валидируется. Любое изменение требует новой policyVersion и golden results.

## Последствия

Пользовательский лимит невозможно компенсировать хорошим score. Tie-break по candidate ID обеспечивает детерминизм. UI может объяснять решение через stable reason codes.
