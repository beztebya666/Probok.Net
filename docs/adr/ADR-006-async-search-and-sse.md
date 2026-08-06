# ADR-006: Асинхронный поиск и SSE

- Статус: принято
- Дата: 2026-08-05

## Контекст

Первичные alternatives обычно быстрые, baseline и detour требуют нескольких ограниченных provider calls. Держать POST открытым ухудшает retry/idempotency и proxy behavior.

## Решение

POST создаёт resource и возвращает `202` с полным accepted envelope. Worker продолжает под search deadline. GET даёт snapshot, SSE — ordered events с monotonic ID, heartbeat и replay через `Last-Event-ID`. DELETE одновременно отменяет context и удаляет authorized state. Edge хранит subject ownership отдельно и маскирует чужой ID как 404.

SSE выбран вместо WebSocket: поток однонаправленный, браузер имеет native reconnect, HTTP infrastructure проще. Polling остаётся fallback.

## Последствия

Events являются progress hints; terminal GET/result остаётся источником истины. Memory state теряется при restart — это явно связано с license-controlled Redis option в ADR-005.
