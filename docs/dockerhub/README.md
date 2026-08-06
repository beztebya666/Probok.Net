# Пробок.Нет

**Маршрут не самый быстрый, а самый зелёный.**

Навигаторы оптимизируют ETA и с удовольствием отправят вас в плотный поток ради выигрыша в две
минуты. Пробок.Нет считает другую величину — **долю пути по свободным участкам** — и целенаправленно
ищет объезд, который эту долю увеличивает.

Отличие от «попросить у провайдера альтернативы»: приложение само ведёт поиск. Оно находит пробки на
маршруте, превращает их в запретные зоны и боковые якоря, просит провайдера проложить маршрут заново
— и так по лестнице всё более агрессивных гипотез, пока не кончится бюджет запросов. Найденные
варианты ранжируются по доле зелени, первые три показываются как доказательство, что поиск был.

Исходники, скриншоты и документация: **https://github.com/beztebya666/Probok.Net**
Живое демо в браузере: **https://beztebya666.github.io/Probok.Net/**

---

## Что здесь лежит

Один репозиторий на все компоненты — компонент указан префиксом тега.

| Тег | Что это | Порт |
|---|---|---|
| `edge-api-<версия>` | публичный API: OIDC, валидация, rate limit, идемпотентность, SSE | 8080 |
| `routing-orchestrator-<версия>` | поиск зелёного коридора, бюджет запросов, ранжирование | 8081 |
| `provider-yandex-<версия>` | адаптер 2GIS/Yandex: retry, circuit breaker, нормализация | 8082 |
| `web-<версия>` | Next.js-клиент | 3000 |

Платформы: `linux/amd64`, `linux/arm64`. Каждый тег существует и как `<компонент>-latest`.

Те же образы опубликованы в GitHub Packages: `ghcr.io/beztebya666/probok.net/<компонент>`.

```bash
docker pull beztebya666/probok.net:edge-api-latest
docker pull beztebya666/probok.net:routing-orchestrator-latest
docker pull beztebya666/probok.net:provider-yandex-latest
docker pull beztebya666/probok.net:web-latest
```

## Быстрый запуск

Компоненты рассчитаны на совместную работу, поэтому проще всего поднять их через Compose из
репозитория — там уже прописаны сеть, health-чеки и порядок запуска:

```bash
git clone https://github.com/beztebya666/Probok.Net
cd Probok.Net
cp .env.example .env
docker compose up --wait
```

Откроется <http://localhost:3000>. В профиле по умолчанию работает synthetic-провайдер — ключи не
нужны.

Отдельный компонент, если он нужен сам по себе:

```bash
docker run --rm -p 8082:8082 \
  -e APP_ENV=local \
  -e PROVIDER_MODE=stub \
  -e INTERNAL_API_TOKEN=local-internal-token-32-characters \
  beztebya666/probok.net:provider-yandex-latest
```

## Реальные провайдеры

```bash
PROVIDER_MODE=2gis
DGIS_API_KEY=...                    # серверный ключ, в браузер не уходит
DGIS_RATE_LIMIT_PER_MINUTE=12       # не меньше бюджета одного поиска
NEXT_PUBLIC_2GIS_MAPGL_API_KEY=...  # отдельный браузерный ключ, ограниченный по origin
```

Серверные ключи никогда не имеют префикса `NEXT_PUBLIC_`. Дневная квота 2GIS Routing API на
бесплатном тарифе — 50 объектов, один зелёный поиск расходует до 8.

## Проверка подписи

Образы, собранные с доступным OIDC, подписаны cosign (keyless):

```bash
cosign verify ghcr.io/beztebya666/probok.net/edge-api:latest \
  --certificate-identity-regexp 'https://github.com/beztebya666/Probok.Net/.*' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

## Лицензия

Проприетарная source-available лицензия: код открыт для чтения, но любое использование, изменение
или распространение — только с письменного разрешения автора. См. `LICENSE` в репозитории.
