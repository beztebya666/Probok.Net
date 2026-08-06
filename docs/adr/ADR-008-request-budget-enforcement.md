# ADR-008: Бюджет provider requests

- Статус: принято
- Дата: 2026-08-05

## Решение

Каждый search создаёт concurrency-safe budget. Перед call worker резервирует bounded allowance, передаёт остаток adapter для retry и после ответа фиксирует фактически использованные outbound requests. Неиспользованный reserve освобождается. При нулевом остатке новые baseline/detour calls не стартуют.

Отдельно считать:

- outbound request count — hard budget;
- billable response units — provider может тарифицировать каждый из нескольких answers;
- monetary cost — только при operator-configured contract tariff, иначе честный 0/unknown.

Enhanced search ограничен также deadline, iteration count, active candidates, zone size и minimum material improvement.

## Последствия

Concurrent baseline evaluation не может oversubscribe budget. Retry storm предотвращён. При исчерпании возвращается лучший уже подтверждённый eligible route с `SEARCH_BUDGET_EXHAUSTED`, а не бесконечная очередь.
