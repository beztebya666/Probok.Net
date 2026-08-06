# Contributing

## Перед изменением

1. Прочитайте соответствующий ADR и `PROVIDER_CAPABILITIES.md`.
2. Не добавляйте реальные provider responses, координаты, tokens или keys в fixtures/logs/issues.
3. Изменение scoring thresholds/weights требует новой `policyVersion`, golden tests и ADR update.
4. Изменение public contract начинается с `docs/api/openapi.yaml`, затем regenerated client и implementation.

## Локальная проверка

```bash
gofmt -w ./internal ./services
go vet ./...
go test ./...
cd apps/web && npm ci && npm run lint && npm run typecheck && npm test && npm run build
```

Дополнительно запустите OpenAPI lint, Compose config, Helm lint и E2E для затронутых путей. Новый provider adapter обязан иметь synthetic contract fixture и sanitized error tests.

## Commit/PR checklist

- нет `TODO`/`panic` в runtime path;
- новые ошибки нормализованы и не содержат payload/URL/key;
- cancellation/deadline propagation сохранены;
- metrics labels bounded и не содержат coordinates/IDs;
- defaults fail closed в production;
- documentation и runbook обновлены;
- lock-файлы и base-image digests зафиксированы.
