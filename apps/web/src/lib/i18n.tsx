"use client";

import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from "react";

export type Locale = "ru" | "en";

const ru = {
  brandName: "Пробок.Нет",
  plannerTitle: "Куда едем?",
  origin: "Откуда",
  destination: "Куда",
  waypoint: "Через",
  addWaypoint: "Добавить точку",
  removeWaypoint: "Удалить промежуточную точку",
  currentLocation: "Моё местоположение",
  geolocationUnavailable: "Не удалось определить местоположение",
  enterAddress: "Начните вводить адрес",
  searchingAddresses: "Ищем адреса…",
  noSuggestions: "Ничего не найдено",
  selectSuggestion: "Выберите адрес из подсказок",
  routeMode: "Режим",
  FASTEST: "Быстрее",
  BALANCED: "Баланс",
  GREENEST: "Свободнее",
  STRICT_GREEN: "Только по зелёному",
  FASTEST_desc: "Минимальный ETA",
  BALANCED_desc: "Время и плавность",
  GREENEST_desc: "Меньше загруженных участков",
  STRICT_GREEN_desc: "Каждый участок подтверждённо зелёный",
  limits: "Допустимый объезд",
  kilometersShort: "км",
  minutesShort: "мин",
  extraDistance: "До {value} км",
  extraTime: "До {value} мин",
  avoidTolls: "Без платных дорог",
  avoidUnpaved: "Без грунтовых дорог",
  buildRoute: "Найти маршрут",
  savedRoutes: "Мои маршруты",
  tabFavorites: "Избранное",
  tabRecent: "Недавние",
  tabBookmarks: "Закладки",
  bookmarksEmpty: "Сохраните разбор — он откроется без запросов к API.",
  favoritesEmpty: "Отметьте маршрут звёздочкой, чтобы он остался здесь.",
  measuredAt: "Актуально на {value}",
  favoriteRoutes: "Избранное",
  recentRoutes: "Недавние",
  recentRoutesEmpty: "Постройте маршрут — он появится здесь для быстрого повтора.",
  allRecentFavorited: "Все недавние маршруты закреплены в избранном.",
  localRoutesPrivacy: "только здесь",
  localRoutesPrivacyTitle: "Маршруты хранятся только в этом браузере и не синхронизируются.",
  restoreRoute: "Заполнить маршрут {value}",
  addFavorite: "Добавить маршрут {value} в избранное",
  removeFavorite: "Убрать маршрут {value} из избранного",
  addFavoriteShort: "В избранное",
  removeFavoriteShort: "Убрать из избранного",
  trafficProvider_off: "без пробок",
  trafficProvider_yandex: "пробки Яндекса",
  trafficProvider_2gis: "пробки 2ГИС",
  searchInProgress: "Ищем лучший вариант",
  cancel: "Отменить",
  cancelSearch: "Отменить поиск",
  searching: "Анализируем варианты",
  progressQueued: "Поиск поставлен в очередь",
  progressInitial: "Получены исходные маршруты",
  progressBaseline: "Сравниваем движение с обычным",
  progressCandidate: "Найден новый вариант",
  progressScoring: "Оцениваем равномерность движения",
  progressFinal: "Обновляем рекомендации",
  progressDone: "Сравнение готово",
  reconnecting: "Восстанавливаем онлайн-обновления",
  polling: "Обновляем статус периодически",
  resultTitle: "Варианты маршрута",
  routesCount: "{value} {plural:вариант|варианта|вариантов}",
  greenTopTitle: "Топ-3 по зелени",
  greenTopSubtitle: "Доля пути по зелёным участкам — по времени в пути.",
  greenTopEmpty: "Пока нет вариантов с подтверждёнными данными о движении.",
  greenRank: "#{value}",
  greenShareTime: "{value}% времени по зелёному",
  greenShareDistance: "{value}% километров по зелёному",
  greenTopHint: "— по фактическим цветам участков; обновить кнопкой сверху.",
  showOnMap: "Показать на карте",
  openRouteIn: "Открыть в",
  open2gis: "2ГИС",
  openYandex: "Яндексе",
  themeToDark: "Тёмная тема",
  themeToLight: "Светлая тема",
  restoredResult: "Предыдущий результат",
  dismissResult: "Убрать результат",
  bookmarksTitle: "Закладки",
  saveBookmark: "Сохранить разбор",
  savedBookmark: "В закладках",
  unsaveBookmark: "Убрать из закладок",
  bookmarkSaved: "Разбор сохранён — открывается без запросов к API",
  openBookmark: "Открыть закладку {value}",
  removeBookmark: "Удалить закладку {value}",
  removeBookmarkShort: "Удалить",
  refreshResult: "Обновить — новый запрос к API",
  refreshFailedKept: "Обновить не удалось — показан сохранённый разбор",
  greenShareShort: "{value}% зелени",
  recommended: "Рекомендуем",
  fastest: "Самый быстрый",
  greenest: "Меньше загруженности",
  selected: "Выбран",
  choose: "Выбрать",
  eta: "В пути",
  distance: "Расстояние",
  delay: "Задержка",
	congestionBreakdown: "Оценка загруженности по времени",
	redDuration: "Красные участки",
	orangeDuration: "Оранжевые участки",
  detour: "+{distance} · +{time}",
  noDetour: "без объезда",
  estimatedFree: "{value}% пути без заметной задержки",
  unknownCoverage: "Для {value}% пути данных недостаточно",
  confidence: "Уверенность",
  confidence_HIGH: "Высокая",
  confidence_MEDIUM: "Средняя",
  confidence_LOW: "Низкая",
  why: "Почему этот вариант",
  tolls: "Есть платные участки",
  unpaved: "Есть грунтовые участки",
  mapRegion: "Карта маршрута",
  mapFallbackTitle: "Схематичная карта",
  mapFallbackBody: "Проверьте ключ JavaScript API и HTTP Referer. Пока маршруты показаны схематично.",
  mapLoading: "Загружаем карту…",
  mapError: "Интерактивная карта недоступна",
  demo: "Демо-данные",
  demoToast: "Это демо — боевые запросы к API выключены",
  degradedTitle: "Расширенный поиск ограничен",
  degradedBody: "Показываем доступные исходные варианты. Рекомендация может быть менее точной.",
  warningTitle: "Важно",
  searchError: "Не удалось построить маршрут",
  retry: "Изменить параметры",
  correlationId: "Код обращения: {value}",
  configTitle: "Frontend не подключён к edge API",
  configBody: "Задайте NEXT_PUBLIC_EDGE_API_BASE_URL или явно включите demo mode для локальной демонстрации.",
  language: "Язык",
  admin: "Состояние системы",
  backToRoute: "К маршрутам",
  adminTitle: "Операционный обзор",
  adminSubtitle: "Агрегированные показатели без пользовательских координат.",
  demoTelemetryTitle: "Реальная телеметрия не подключена",
  demoTelemetryBody: "В демо-режиме мы не подставляем выдуманные запросы, расходы и статусы провайдера. Здесь появятся только фактические показатели edge API после подключения мониторинга.",
  accessChecking: "Проверяем права доступа…",
  accessDenied: "Доступ не подтверждён",
  accessDeniedBody: "Требуется роль admin. Сервер повторно проверяет права для каждого запроса.",
  providerStatus: "Провайдер",
  circuitBreaker: "Circuit breaker",
  requestCount: "Запросы",
  estimatedCost: "Оценочная стоимость",
  scoringPolicy: "Политика оценки",
  degradedPercent: "Degraded результаты",
  lowConfidence: "Низкий confidence",
  budgetExhaustion: "Исчерпан бюджет",
  featureFlags: "Feature flags",
  refresh: "Обновить",
  operationalData: "Операционные данные",
  configurationError: "Конфигурация недоступна",
  offline: "Нет сети",
  online: "Связь восстановлена",
};

const en: typeof ru = {
  brandName: "Пробок.Нет",
  plannerTitle: "Where to?",
  origin: "From",
  destination: "To",
  waypoint: "Via",
  addWaypoint: "Add stop",
  removeWaypoint: "Remove waypoint",
  currentLocation: "My location",
  geolocationUnavailable: "Could not determine your location",
  enterAddress: "Start typing an address",
  searchingAddresses: "Searching addresses…",
  noSuggestions: "No places found",
  selectSuggestion: "Select an address from the suggestions",
  routeMode: "Mode",
  FASTEST: "Fastest",
  BALANCED: "Balanced",
  GREENEST: "Greenest",
  STRICT_GREEN: "All-green only",
  FASTEST_desc: "Minimum ETA",
  BALANCED_desc: "Time and smoothness",
  GREENEST_desc: "Fewer congested sections",
  STRICT_GREEN_desc: "Every segment must be confirmed green",
  limits: "Detour allowance",
  kilometersShort: "km",
  minutesShort: "min",
  extraDistance: "Up to {value} km",
  extraTime: "Up to {value} min",
  avoidTolls: "Avoid toll roads",
  avoidUnpaved: "Avoid unpaved roads",
  buildRoute: "Find routes",
  savedRoutes: "My routes",
  tabFavorites: "Favourites",
  tabRecent: "Recent",
  tabBookmarks: "Bookmarks",
  bookmarksEmpty: "Save an analysis and it reopens without API requests.",
  favoritesEmpty: "Star a route to keep it here.",
  measuredAt: "Measured at {value}",
  favoriteRoutes: "Favourites",
  recentRoutes: "Recent",
  recentRoutesEmpty: "Build a route and it will appear here for quick reuse.",
  allRecentFavorited: "All recent routes are pinned to favourites.",
  localRoutesPrivacy: "only here",
  localRoutesPrivacyTitle: "Routes are stored only in this browser and are not synchronised.",
  restoreRoute: "Restore route {value}",
  addFavorite: "Add route {value} to favourites",
  removeFavorite: "Remove route {value} from favourites",
  addFavoriteShort: "Add to favourites",
  removeFavoriteShort: "Remove from favourites",
  trafficProvider_off: "traffic off",
  trafficProvider_yandex: "Yandex traffic",
  trafficProvider_2gis: "2GIS traffic",
  searchInProgress: "Finding the best option",
  cancel: "Cancel",
  cancelSearch: "Cancel search",
  searching: "Analysing options",
  progressQueued: "Search queued",
  progressInitial: "Initial routes received",
  progressBaseline: "Comparing live and typical flow",
  progressCandidate: "A new option was found",
  progressScoring: "Scoring traffic smoothness",
  progressFinal: "Updating recommendations",
  progressDone: "Comparison ready",
  reconnecting: "Restoring live updates",
  polling: "Checking status periodically",
  resultTitle: "Route options",
  routesCount: "{value} {plural:option|options|options}",
  greenTopTitle: "Top 3 by green share",
  greenTopSubtitle: "Share of the trip spent on free-flowing sections, by driving time.",
  greenTopEmpty: "No option with confirmed traffic evidence yet.",
  greenRank: "#{value}",
  greenShareTime: "{value}% of the time on green",
  greenShareDistance: "{value}% of the distance on green",
  greenTopHint: "— from the observed segment colours; refresh above.",
  showOnMap: "Show on map",
  openRouteIn: "Open in",
  open2gis: "2GIS",
  openYandex: "Yandex",
  themeToDark: "Dark theme",
  themeToLight: "Light theme",
  restoredResult: "Previous result",
  dismissResult: "Dismiss result",
  bookmarksTitle: "Bookmarks",
  saveBookmark: "Save analysis",
  savedBookmark: "Bookmarked",
  unsaveBookmark: "Remove from bookmarks",
  bookmarkSaved: "Analysis saved — reopening it costs no API requests",
  openBookmark: "Open bookmark {value}",
  removeBookmark: "Remove bookmark {value}",
  removeBookmarkShort: "Remove",
  refreshResult: "Refresh — makes a new API request",
  refreshFailedKept: "Refresh failed — the saved analysis is shown",
  greenShareShort: "{value}% green",
  recommended: "Recommended",
  fastest: "Fastest",
  greenest: "Lower congestion",
  selected: "Selected",
  choose: "Select",
  eta: "Travel time",
  distance: "Distance",
  delay: "Delay",
	congestionBreakdown: "Estimated congestion duration",
	redDuration: "Red sections",
	orangeDuration: "Orange sections",
  detour: "+{distance} · +{time}",
  noDetour: "no detour",
  estimatedFree: "{value}% with no material estimated delay",
  unknownCoverage: "Insufficient data for {value}% of this route",
  confidence: "Confidence",
  confidence_HIGH: "High",
  confidence_MEDIUM: "Medium",
  confidence_LOW: "Low",
  why: "Why this option",
  tolls: "Includes toll sections",
  unpaved: "Includes unpaved sections",
  mapRegion: "Route map",
  mapFallbackTitle: "Schematic map",
  mapFallbackBody: "Check the JavaScript API key and HTTP Referer. Routes are shown schematically for now.",
  mapLoading: "Loading map…",
  mapError: "Interactive map unavailable",
  demo: "Demo data",
  demoToast: "Demo build — live API requests are disabled",
  degradedTitle: "Enhanced search is limited",
  degradedBody: "Available initial options are shown. The recommendation may be less precise.",
  warningTitle: "Important",
  searchError: "Could not build a route",
  retry: "Change parameters",
  correlationId: "Support code: {value}",
  configTitle: "Frontend is not connected to edge API",
  configBody: "Set NEXT_PUBLIC_EDGE_API_BASE_URL or explicitly enable demo mode for a local demonstration.",
  language: "Language",
  admin: "System status",
  backToRoute: "Back to routes",
  adminTitle: "Operations overview",
  adminSubtitle: "Aggregated metrics without user coordinates.",
  demoTelemetryTitle: "Live telemetry is not connected",
  demoTelemetryBody: "Demo mode never invents request, cost, or provider-health values. Only actual edge API metrics will appear here after monitoring is connected.",
  accessChecking: "Checking access…",
  accessDenied: "Access not confirmed",
  accessDeniedBody: "The admin role is required. The server checks authorization again for every request.",
  providerStatus: "Provider",
  circuitBreaker: "Circuit breaker",
  requestCount: "Requests",
  estimatedCost: "Estimated cost",
  scoringPolicy: "Scoring policy",
  degradedPercent: "Degraded results",
  lowConfidence: "Low confidence",
  budgetExhaustion: "Budget exhausted",
  featureFlags: "Feature flags",
  refresh: "Refresh",
  operationalData: "Operational data",
  configurationError: "Configuration unavailable",
  offline: "Offline",
  online: "Connection restored",
};

type TranslationKey = keyof typeof ru;
type Replacements = Record<string, string | number>;

/**
 * Picks the plural form for `value` from a `{plural:one|few|many}` placeholder.
 * Russian needs three forms ("1 вариант", "2 варианта", "5 вариантов"); a single
 * hardcoded form is wrong for most numbers, including zero.
 */
function pluralize(locale: Locale, count: number, forms: string[]): string {
  const [one = "", few = "", many = ""] = forms;
  if (locale !== "ru") return Math.abs(count) === 1 ? one : few || many;
  const absolute = Math.abs(count) % 100;
  const units = absolute % 10;
  if (absolute > 10 && absolute < 20) return many;
  if (units > 1 && units < 5) return few;
  if (units === 1) return one;
  return many;
}

export function translate(locale: Locale, key: TranslationKey, replacements?: Replacements): string {
  let value: string = (locale === "ru" ? ru : en)[key];
  if (replacements) {
    const count = Number(replacements["value"]);
    if (Number.isFinite(count)) {
      value = value.replace(/\{plural:([^}]*)\}/g, (_, forms: string) => pluralize(locale, count, forms.split("|")));
    }
    for (const [name, replacement] of Object.entries(replacements)) {
      value = value.replaceAll(`{${name}}`, String(replacement));
    }
  }
  return value;
}

type LocaleContextValue = {
  locale: Locale;
  setLocale: (locale: Locale) => void;
  t: (key: TranslationKey, replacements?: Replacements) => string;
};

const LocaleContext = createContext<LocaleContextValue | undefined>(undefined);

export function LocaleProvider({ children }: { children: ReactNode }) {
  const [locale, setLocaleState] = useState<Locale>("ru");

  useEffect(() => {
    let stored: string | null = null;
    try {
      if (typeof window.localStorage?.getItem === "function") stored = window.localStorage.getItem("greenroute.locale");
    } catch {
      stored = null;
    }
    const detected: Locale = stored === "en" || stored === "ru" ? stored : navigator.language.toLowerCase().startsWith("ru") ? "ru" : "en";
    document.documentElement.lang = detected;
    window.queueMicrotask(() => setLocaleState(detected));
  }, []);

  const setLocale = (next: Locale) => {
    setLocaleState(next);
    document.documentElement.lang = next;
    try {
      if (typeof window.localStorage?.setItem === "function") window.localStorage.setItem("greenroute.locale", next);
    } catch {
      // Language still changes for the current session when storage is unavailable.
    }
  };

  const value = useMemo<LocaleContextValue>(
    () => ({ locale, setLocale, t: (key, replacements) => translate(locale, key, replacements) }),
    [locale],
  );

  return <LocaleContext.Provider value={value}>{children}</LocaleContext.Provider>;
}

export function useLocale(): LocaleContextValue {
  const context = useContext(LocaleContext);
  if (!context) throw new Error("useLocale must be used within LocaleProvider");
  return context;
}

export function reasonLabel(code: string, locale: Locale): string {
  const labels: Record<string, [string, string]> = {
    FASTEST_REFERENCE: ["Самый быстрый исходный вариант", "Fastest initial option"],
    TRAFFIC_DELAY_PRESENT: ["Есть участки с заметной задержкой", "Material traffic delay detected"],
    LOW_RED_EXPOSURE: ["Меньше времени на сильно загруженных участках", "Less time on heavily congested sections"],
    WITHIN_DETOUR_LIMIT: ["Объезд укладывается в заданный лимит", "Detour stays within your limit"],
    STABLE_FLOW: ["Более равномерная скорость по маршруту", "More consistent flow along the route"],
    SHORT_DETOUR: ["Небольшой объезд относительно быстрого маршрута", "Short detour versus the fastest route"],
    PARTIAL_BASELINE_COVERAGE: ["Типичные данные доступны не для всего пути", "Typical-flow data covers only part of the route"],
    SUFFICIENT_BASELINE_COVERAGE: ["Достаточное покрытие типичными данными", "Sufficient typical-flow coverage"],
    INSUFFICIENT_BASELINE_DATA: ["Недостаточно данных для уверенной оценки", "Insufficient data for a confident estimate"],
    GEOMETRY_MATCH_CONFIRMED: ["Сопоставление участков подтверждено", "Segment matching confirmed"],
    PROVIDER_ROUTE_DETAILS: ["Маршрут построен по официальным данным провайдера", "Built from the provider's official route data"],
    OFFICIAL_PROVIDER_ROUTE: ["Официальный маршрут картографического провайдера", "Official map-provider route"],
    SEGMENT_CONGESTION_UNKNOWN: ["Загруженность отдельных участков не раскрыта провайдером", "Per-segment congestion is not exposed by the provider"],
    NO_OFFICIAL_SEGMENT_CONGESTION_CLASS: ["Нет официальной классификации пробок по участкам", "No official per-segment congestion classification"],
    PROVIDER_HAS_NO_OFFICIAL_SEGMENT_CONGESTION_CLASS: ["Провайдер не передаёт официальную классификацию пробок по участкам", "The provider does not expose official per-segment congestion classes"],
    DGIS_TRAFFIC_FORECAST: ["2ГИС учитывает актуальную и прогнозную дорожную ситуацию в ETA", "2GIS includes current and forecast traffic in ETA"],
    DGIS_TRAFFIC_REALTIME: ["2ГИС учитывает текущую дорожную ситуацию в ETA", "2GIS includes current traffic in ETA"],
    PARTIAL_TRAFFIC_DATA: ["Данных недостаточно для оценки каждого участка", "Traffic evidence does not cover every segment"],
    NO_BASELINE_LIVE_PAIR: ["Нет сопоставимой пары обычного и текущего времени в пути", "No comparable typical/live travel-time pair"],
    TRAFFIC_FRESHNESS_UNKNOWN: ["Время последнего обновления трафика не опубликовано", "Traffic freshness is not published"],
    LOW_CONFIDENCE_BASELINE: ["Оценка дорожной ситуации имеет низкую уверенность", "Traffic estimate confidence is low"],
    WITHIN_EXTRA_DISTANCE_LIMIT: ["Маршрут укладывается в лимит объезда", "Route stays within the detour limit"],
    NO_GREEN_ROUTE_AVAILABLE: ["Подтвердить полностью свободный маршрут не удалось", "A fully free-flowing route could not be confirmed"],
  };
  const label = labels[code];
  return label ? label[locale === "ru" ? 0 : 1] : code.replaceAll("_", " ").toLocaleLowerCase(locale);
}

export function warningLabel(code: string, message: string | undefined, locale: Locale): string {
  const labels: Record<string, [string, string]> = {
    PARTIAL_BASELINE_COVERAGE: ["Часть участков имеет пониженную уверенность оценки.", "Some sections have lower estimate confidence."],
    PROVIDER_DEGRADED: ["Картографический провайдер отвечает нестабильно.", "The map provider is responding unreliably."],
    GREEN_OPTIMIZATION_UNAVAILABLE: ["Green-оптимизация временно недоступна.", "Green optimization is temporarily unavailable."],
    SEARCH_BUDGET_EXHAUSTED: ["Лимит внешних запросов исчерпан; показан лучший найденный вариант.", "The provider-request budget was exhausted; the best available option is shown."],
    PROVIDER_INITIAL_CANDIDATES_UNAVAILABLE: ["Провайдер маршрутов временно не вернул исходные варианты.", "The route provider temporarily returned no initial alternatives."],
    PROVIDER_SEGMENT_CONGESTION_UNAVAILABLE: ["2ГИС учёл текущую дорожную ситуацию во времени поездки, но не предоставляет официальную загруженность каждого участка.", "2GIS accounted for current traffic in travel time but does not expose official congestion for every segment."],
    PROVIDER_BILLING_UNITS_ESTIMATED: ["Расход лимита 2ГИС рассчитан по числу запросов; точное списание отображается в кабинете провайдера.", "2GIS usage is estimated from requests; exact billing is available in the provider dashboard."],
    ROUTES_REJECTED_BY_HARD_CONSTRAINTS: ["Часть вариантов не уложилась в выбранные ограничения объезда.", "Some alternatives exceeded the selected detour constraints."],
    NO_ROUTE_WITHIN_HARD_CONSTRAINTS: ["В заданных пределах объезда более свободный маршрут не найден — показан самый быстрый доступный вариант.", "No freer route fits the detour limits, so the fastest available option is shown."],
    LOW_CONFIDENCE_RESULT: ["Уверенность оценки низкая: маршрут можно использовать, но цветовую оценку участков следует считать неполной.", "Estimate confidence is low: the route is usable, but its segment colors are incomplete."],
    NO_GREEN_ROUTE_AVAILABLE: ["По доступным данным подтвердить полностью свободный маршрут не удалось.", "The available evidence cannot confirm a fully free-flowing route."],
    BASELINE_EVALUATION_UNAVAILABLE: ["Провайдер не вернул данные для сравнения с обычным временем в пути; показан исходный маршрут.", "The provider returned no typical-travel-time comparison; the initial route is shown."],
    NO_VALID_PROVIDER_ROUTES: ["Провайдер не вернул маршрут с корректной геометрией.", "The provider returned no route with valid geometry."],
    INVALID_PROVIDER_CANDIDATES_REJECTED: ["Некорректные варианты провайдера исключены из результатов.", "Invalid provider alternatives were excluded."],
    ENHANCED_SEARCH_PROVIDER_ERROR: ["Дополнительный поиск объездов завершился досрочно; показаны уже найденные варианты.", "The extended detour search ended early; existing alternatives are shown."],
  };
  const label = labels[code];
  if (label) return label[locale === "ru" ? 0 : 1];
  return message || reasonLabel(code, locale);
}
