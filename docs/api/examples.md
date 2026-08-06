# Примеры публичного API

Локальный профиль включает анонимный доступ. В production добавьте `Authorization: Bearer <short-lived-access-token>`.

## Запуск поиска

```bash
curl --fail-with-body http://localhost:8080/api/v1/route-searches \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: example-search-0001' \
  --data '{
    "origin":{"latitude":55.751244,"longitude":37.618423},
    "destination":{"latitude":55.8319,"longitude":37.4116},
    "waypoints":[],
    "routingMode":"GREENEST",
    "maxExtraDistanceMeters":30000,
    "maxExtraDistancePercent":50,
    "maxExtraTimeSeconds":1200,
    "avoidTolls":false,
    "avoidUnpaved":false,
    "strictness":0.8,
    "maxProviderRequests":8,
    "searchDeadlineMs":10000
  }'
```

## Чтение состояния и SSE replay

```bash
curl --fail-with-body http://localhost:8080/api/v1/route-searches/SEARCH_ID
curl -N http://localhost:8080/api/v1/route-searches/SEARCH_ID/events
curl -N http://localhost:8080/api/v1/route-searches/SEARCH_ID/events \
  -H 'Last-Event-ID: 4'
```

## Поиск адреса и удаление state

```bash
curl --get http://localhost:8080/api/v1/geosuggest \
  --data-urlencode 'q=Москва, Кремль' --data 'lang=ru_RU' --data 'limit=5'
curl -X DELETE http://localhost:8080/api/v1/route-searches/SEARCH_ID
```

Цвета и классы сегментов в ответе — собственная эвристика Пробок.Нет. `UNKNOWN` означает недостаточность данных и не интерпретируется как свободная дорога.
