# ADR-007: Сопоставление live и baseline геометрии

- Статус: принято
- Дата: 2026-08-05

## Решение

Baseline строится через упрощённые control points исходного live corridor, не превышая provider waypoint limit. Сходство объединяет symmetric sampled Hausdorff distance, corridor overlap и length similarity. Dedupe дополнительно требует близость длины и сохраняет fastest deterministic candidate.

Если similarity ниже policy threshold, segment получает `UNKNOWN`, baseline не используется как доказательство свободности, confidence понижается. Provider может вернуть иной legal route даже через points; этот случай нормален и не исправляется искусственным duration mapping.

## Последствия

Алгоритм O(n²) по densified points, но polyline заранее упрощается и active candidates bounded. Для межгородских сверхдлинных маршрутов threshold и sampling требуют отдельной calibration dataset.
