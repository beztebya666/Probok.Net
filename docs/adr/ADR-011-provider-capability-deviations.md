# ADR-011: Отклонения ожиданий продукта от Yandex Route Details API

- Статус: принято
- Дата: 2026-08-05

## Решение

Фактическая официальная документация имеет приоритет. Обнаруженные расхождения:

| Ожидание | Документированный факт | Коррекция |
|---|---|---|
| «Максимально доступные альтернативы» без числа | `results=1..3`, может вернуться меньше, paid feature | Максимум 2 альтернативы + основной маршрут. |
| Официальная загруженность каждого сегмента | В schema есть duration/length/polyline, но нет congestion class/jam score | Собственная оценка после live/baseline matching; до него `UNKNOWN`. |
| Baseline той же геометрии | `traffic=disabled` оптимизирует кратчайшее расстояние | Отдельный запрос и geometry matching; совпадение не предполагается. |
| Departure time для любого расчёта | Не в прошлом; игнорируется при disabled traffic | Запрещаем сочетание departure + disabled во внутреннем запросе. |
| Request = единица стоимости | При нескольких Ответах на один Запрос оплачивается каждый Ответ | Разделяем outbound `requestsUsed` и `estimatedBillableUnits`. |
| Basic/Advanced | Официальные названия — Standard/Extended | В UI/docs используем официальные названия; mapping помечен как внутренний. |
| Extended однозначно разрешает любую модификацию | Публичные описания и специальные условия формулируют право неодинаково | Modification default false; точное право contract-dependent. |
| Geosuggest сразу даёт точку | Suggest response даёт title/subtitle/URI, но не координаты | Point-bearing endpoint реализуем через официальный HTTP Geocoder с отдельной лицензией/ключом; Suggest не выдаём за координатный API. |
| Avoid-zones полностью специфицированы | Есть multiple polygons и минимум 3 точки, но нет опубликованного максимума | Provider limit `UNKNOWN`; внутренний safety cap 8 × 32. |

Источники: [Router request](https://yandex.ru/maps-api/docs/router-api/request.html),
[Router response](https://yandex.ru/maps-api/docs/router-api/response.html),
[Router terms](https://yandex.ru/dev/tariffs/doc/ru/router/terms/),
[Router prices](https://yandex.ru/dev/tariffs/doc/ru/router/prices/),
[Suggest response](https://yandex.ru/maps-api/docs/suggest-api/response.html),
[Geocoder request](https://yandex.com/maps-api/docs/geocoder-api/request.html),
[Geocoder response](https://yandex.com/maps-api/docs/geocoder-api/response.html).

## Experimental sources

Для localhost допускается архитектурный extension point
`ALLOW_EXPERIMENTAL_PROVIDER_SOURCES`, но текущая реализация намеренно не содержит
scraping, перехват внутренних запросов или undocumented endpoints. Флаг inert,
по умолчанию `false`, capability остаётся `experimentalSources=false`. Такой
источник был бы brittle, не имел бы стабильной schema/лицензионной опоры и не
может стать production fallback без нового ADR, отдельного adapter и явного
разрешения на данные.
