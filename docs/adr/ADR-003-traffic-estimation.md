# ADR-003: Оценка трафика через парные live/baseline маршруты

- Статус: принято
- Дата: 2026-08-05

## Контекст

Yandex Route Details API возвращает использованный `traffic_type`, длительность,
длину и геометрию steps. Документированного jam score/цвета пробок и одновременно
live/baseline времени одного segment нет
([response](https://yandex.ru/maps-api/docs/router-api/response.html)). При
`traffic=disabled` маршрут строится по кратчайшему расстоянию, поэтому его
геометрия не обязана совпасть с live-маршрутом
([request](https://yandex.ru/maps-api/docs/router-api/request.html#traffic)).

## Решение

1. Получать initial candidates с `traffic=true`; принимать `traffic_type` только
   как описание использованной Яндексом модели (`realtime`/`forecast`), не как
   официальный уровень загруженности.
2. При достаточном request budget orchestrator делает отдельный вызов того же
   adapter с `traffic=false` и получает baseline candidates.
3. Сопоставлять geometry отдельно в scoring/domain layer. Provider service не
   притворяется, что разные геометрии — один и тот же участок.
4. Рассчитывать delay/ratio/class только для достаточно похожих участков. До
   успешного matching ставить `CongestionClass=UNKNOWN`, confidence снижать и
   добавлять reason code.
5. Никогда не преобразовывать отсутствие baseline в `GREEN`. В частности,
   короткий `duration` сам по себе не доказывает свободную дорогу.
6. `departure_time` передавать только в traffic-enabled запросе и никогда не в
   прошлом; API игнорирует его при `traffic=disabled`
   ([departure_time](https://yandex.ru/maps-api/docs/router-api/request.html#departure-time)).

Provider adapter заполняет только `liveDurationSeconds` для live/forecast либо
только `baselineDurationSeconds` для disabled ответа. Исключение — synthetic
stub, который может детерминированно сгенерировать обе величины и явно помечен
`SYNTHETIC_STUB`.

## Последствия

- Один baseline round стоит дополнительный outbound attempt и billable response;
  retry невозможен после исчерпания request budget.
- Возможна низкая уверенность или отсутствие классификации при сильном
  расхождении геометрии — это ожидаемый честный результат.
- Нельзя называть вычисленный `trafficRatio` или классы GREEN/RED официальными
  пробками Яндекса.
- Сырые provider responses не нужны для scoring и не сохраняются.

## Отклонённые варианты

- Считать `liveDuration - freeFlow` из недокументированного поля: такого поля в
  официальной схеме нет.
- Сравнивать маршруты только по индексам steps: границы steps нестабильны между
  запросами.
- Считать любой `traffic=disabled` маршрут baseline для live geometry без
  matching: приводит к ложной уверенности.
