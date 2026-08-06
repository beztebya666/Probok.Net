# ADR-001: Границы сервисов и внутренний транспорт

- Статус: принято
- Дата: 2026-08-05

## Контекст

Нужны independently deployable provider isolation, публичная security boundary и ресурсоёмкая orchestration, но отдельный scoring deployment без доказанной нагрузки создаёт distributed monolith.

## Решение

Выделить `web`, `edge-api`, `routing-orchestrator`, `provider-yandex`. Scoring и geometry остаются чистыми внутренними пакетами orchestrator с provider-neutral types и без I/O. Edge и orchestrator не импортируют provider-specific DTO.

Public API — REST/OpenAPI 3.1 и SSE. Внутренний v1 — строгий JSON/HTTP с отдельным token, schema tests, deadlines и trace propagation. Контракты лежат в `internal/contracts`, provider дополнительно публикует внутренний OpenAPI. ConnectRPC является допустимой последующей заменой transport; domain boundary от этого не меняется.

## Последствия

- Provider outage/circuit изолирован от public edge.
- Edge и provider масштабируются независимо; orchestrator state strategy зависит от license ADR-005.
- Scoring можно выделить в deployment только после profiling/load evidence.
- JSON дороже protobuf, но route payload/latency определяются provider I/O, а contract остаётся диагностируемым и простым для synthetic testing.
