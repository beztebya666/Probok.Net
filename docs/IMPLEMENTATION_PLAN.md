# Реализационный план и статус

1. **Capability/architecture** — provider matrix, domain model, public/internal OpenAPI, ADR. Реализовано.
2. **Provider/base product** — official Yandex adapter, Geocoder, deterministic stub, initial routes, map UI. Реализовано; credentialed smoke ожидает project keys.
3. **Traffic model** — live/baseline matching, thresholds, confidence, lexicographic ranking, golden dataset. Реализовано.
4. **Enhanced async search** — budgets, avoid zones, corridor anchors, dedupe, stop conditions, SSE/replay/cancel. Реализовано с feature flags.
5. **Enterprise controls** — OIDC, idempotency, rate limits, privacy audit, resilience, OTel, hardened Kubernetes, CI/CD. Реализовано.
6. **Verification/handoff** — unit/contract/integration/frontend/OpenAPI/Helm/Compose/load checks, runbooks, acceptance report. Проверки выполняются в CI; credentialed provider and production load remain environment gates.

Незавершённые внешние gates не маскируются: они перечислены в `docs/RISKS.md` и acceptance report.
