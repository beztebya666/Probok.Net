"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { useLocale } from "@/lib/i18n";
import { isVerifiedAllGreenRoute, routeCandidatesForMode, segmentColor } from "@/lib/route-insights";
import { TileMap } from "./tile-map";
import type { TrafficProvider } from "@/lib/route-presets";
import type { GeoPoint, RouteCandidate, RouteSearchResult, RoutingMode } from "@/lib/schemas";
import { useResolvedTheme } from "@/lib/use-resolved-theme";
import { CrosshairIcon } from "./icons";

type LngLat = [number, number];
type LngLatBounds = [LngLat, LngLat];
type MapEntity = unknown;
type MapLocation = {
  bounds?: LngLatBounds;
  center?: LngLat;
  duration?: number;
  easing?: "linear" | "ease" | "ease-in" | "ease-out" | "ease-in-out";
  zoom?: number;
};
type YandexMap3 = {
  readonly zoom: number;
  addChild: (entity: MapEntity) => YandexMap3;
  removeChild: (entity: MapEntity) => YandexMap3;
  setLocation: (location: MapLocation) => void;
  destroy: () => void;
};
type Ymaps3 = {
  import: (packageName: string) => Promise<Record<string, unknown>>;
  ready: PromiseLike<void>;
  YMap: new (container: HTMLElement, props: Record<string, unknown>) => YandexMap3;
  YMapDefaultSchemeLayer: new (props?: Record<string, unknown>) => MapEntity;
  YMapDefaultFeaturesLayer: new (props?: Record<string, unknown>) => MapEntity;
  YMapFeature: new (props: Record<string, unknown>) => MapEntity;
  YMapMarker: new (props: Record<string, unknown>, element?: HTMLElement) => MapEntity;
};

type TwoGisObject = { destroy: () => void };
type TwoGisMap = {
  destroy: () => void;
  fitBounds: (
    bounds: { northEast: LngLat; southWest: LngLat },
    options?: { padding?: { top: number; right: number; bottom: number; left: number } },
  ) => void;
  getZoom: () => number;
  setCenter: (center: LngLat, options?: { duration?: number }) => void;
  setZoom: (zoom: number, options?: { duration?: number }) => void;
};
type TwoGisMapGL = {
  isSupported?: () => boolean;
  Map: new (container: HTMLElement | string, options: Record<string, unknown>) => TwoGisMap;
  Polyline: new (map: TwoGisMap, options: Record<string, unknown>) => TwoGisObject;
  HtmlMarker: new (map: TwoGisMap, options: Record<string, unknown>) => TwoGisObject;
};

declare global {
  interface Window {
    mapgl?: TwoGisMapGL;
    ymaps3?: Ymaps3;
  }
}

type YandexRuntime = {
  api: Ymaps3;
  kind: "yandex";
  map: YandexMap3;
  routeEntities: MapEntity[];
  trafficEntities: MapEntity[];
  userLocationMarker?: MapEntity;
};

type TwoGisRuntime = {
  api: TwoGisMapGL;
  kind: "2gis";
  map: TwoGisMap;
  routeEntities: TwoGisObject[];
  userLocationMarker?: TwoGisObject;
};

type MapRuntime = YandexRuntime | TwoGisRuntime;

export type TrafficRenderer = TrafficProvider;

let loaderPromise: Promise<Ymaps3> | undefined;
let twoGisLoaderPromise: Promise<TwoGisMapGL> | undefined;
const MAX_RENDERED_ROUTES = 8;
const MAX_RENDERED_POINTS_PER_LINE = 512;
const MAX_RENDERED_SEGMENTS = 256;
const DEFAULT_CENTER: LngLat = [37.6176, 55.7558];
const DEFAULT_ZOOM = 11;

function boundedItems<T>(items: T[], maximum: number): T[] {
  if (items.length <= maximum) return items;
  return Array.from({ length: maximum }, (_, index) => items[Math.round((index * (items.length - 1)) / (maximum - 1))]!);
}

function renderGeometry(points: GeoPoint[]): GeoPoint[] {
  return boundedItems(points, MAX_RENDERED_POINTS_PER_LINE);
}

function yandexCoordinates(points: GeoPoint[]): LngLat[] {
  return renderGeometry(points).map((point) => [point.longitude, point.latitude]);
}

function renderedRoutes(routes: RouteCandidate[], selectedRoute?: RouteCandidate): RouteCandidate[] {
  const ordered = selectedRoute ? [selectedRoute, ...routes] : routes;
  return [...new Map(ordered.map((route) => [route.candidateId, route])).values()].slice(0, MAX_RENDERED_ROUTES);
}

function waitUntilReady(api: Ymaps3): Promise<Ymaps3> {
  return Promise.resolve(api.ready).then(() => api);
}

export function loadYandexMaps(browserKey: string, locale: "ru" | "en"): Promise<Ymaps3> {
  if (window.ymaps3) return waitUntilReady(window.ymaps3);
  if (loaderPromise) return loaderPromise;

  loaderPromise = new Promise<Ymaps3>((resolve, reject) => {
    const script = document.createElement("script");
    const language = locale === "ru" ? "ru_RU" : "en_US";
    script.src = `https://api-maps.yandex.ru/v3/?apikey=${encodeURIComponent(browserKey)}&lang=${language}`;
    script.async = true;
    script.referrerPolicy = "strict-origin-when-cross-origin";

    let settled = false;
    const timeout = window.setTimeout(() => finishWithError("YANDEX_MAP_LOAD_TIMEOUT"), 15_000);
    const cleanupScript = () => {
      window.clearTimeout(timeout);
      script.onload = null;
      script.onerror = null;
      script.remove();
    };
    const finishWithError = (code: string) => {
      if (settled) return;
      settled = true;
      cleanupScript();
      loaderPromise = undefined;
      reject(new Error(code));
    };
    const finishWithAPI = (api: Ymaps3) => {
      if (settled) return;
      settled = true;
      cleanupScript();
      loaderPromise = undefined;
      resolve(api);
    };

    script.onload = () => {
      const api = window.ymaps3;
      if (!api) {
        finishWithError("YANDEX_MAP_API_MISSING");
        return;
      }
      void waitUntilReady(api).then(finishWithAPI, () => finishWithError("YANDEX_MAP_READY_FAILED"));
    };
    script.onerror = () => finishWithError("YANDEX_MAP_LOAD_FAILED");
    document.head.appendChild(script);
  });
  return loaderPromise;
}

export function loadTwoGisMapGL(): Promise<TwoGisMapGL> {
  if (window.mapgl) return Promise.resolve(window.mapgl);
  if (twoGisLoaderPromise) return twoGisLoaderPromise;

  twoGisLoaderPromise = new Promise<TwoGisMapGL>((resolve, reject) => {
    const script = document.createElement("script");
    script.src = "https://mapgl.2gis.com/api/js/v1";
    script.async = true;
    script.referrerPolicy = "strict-origin-when-cross-origin";

    let settled = false;
    const timeout = window.setTimeout(() => finishWithError("TWOGIS_MAP_LOAD_TIMEOUT"), 15_000);
    const cleanupScript = () => {
      window.clearTimeout(timeout);
      script.onload = null;
      script.onerror = null;
      script.remove();
    };
    const finishWithError = (code: string) => {
      if (settled) return;
      settled = true;
      cleanupScript();
      twoGisLoaderPromise = undefined;
      reject(new Error(code));
    };
    const finishWithAPI = (api: TwoGisMapGL) => {
      if (settled) return;
      settled = true;
      cleanupScript();
      twoGisLoaderPromise = undefined;
      resolve(api);
    };

    script.onload = () => {
      const api = window.mapgl;
      if (!api) {
        finishWithError("TWOGIS_MAP_API_MISSING");
        return;
      }
      finishWithAPI(api);
    };
    script.onerror = () => finishWithError("TWOGIS_MAP_LOAD_FAILED");
    document.head.appendChild(script);
  });
  return twoGisLoaderPromise;
}

function routeBounds(routes: RouteCandidate[]): LngLatBounds | undefined {
  let minLongitude = Number.POSITIVE_INFINITY;
  let maxLongitude = Number.NEGATIVE_INFINITY;
  let minLatitude = Number.POSITIVE_INFINITY;
  let maxLatitude = Number.NEGATIVE_INFINITY;
  for (const route of routes) {
    for (const point of route.geometry) {
      minLongitude = Math.min(minLongitude, point.longitude);
      maxLongitude = Math.max(maxLongitude, point.longitude);
      minLatitude = Math.min(minLatitude, point.latitude);
      maxLatitude = Math.max(maxLatitude, point.latitude);
    }
  }
  if (!Number.isFinite(minLongitude)) return undefined;
  const longitudePadding = Math.max((maxLongitude - minLongitude) * 0.06, 0.002);
  const latitudePadding = Math.max((maxLatitude - minLatitude) * 0.06, 0.002);
  return [
    [minLongitude - longitudePadding, minLatitude - latitudePadding],
    [maxLongitude + longitudePadding, maxLatitude + latitudePadding],
  ];
}

function SchematicMap({ routes, selectedRoute, label, hidden }: { routes: RouteCandidate[]; selectedRoute?: RouteCandidate | undefined; label: string; hidden?: boolean }) {
  const bounds = useMemo(() => {
    const location = { minX: Number.POSITIVE_INFINITY, maxX: Number.NEGATIVE_INFINITY, minY: Number.POSITIVE_INFINITY, maxY: Number.NEGATIVE_INFINITY };
    for (const route of routes) {
      for (const point of route.geometry) {
        location.minX = Math.min(location.minX, point.longitude);
        location.maxX = Math.max(location.maxX, point.longitude);
        location.minY = Math.min(location.minY, point.latitude);
        location.maxY = Math.max(location.maxY, point.latitude);
      }
    }
    return Number.isFinite(location.minX) ? location : { minX: 37.48, maxX: 37.75, minY: 55.68, maxY: 55.88 };
  }, [routes]);
  const project = (point: GeoPoint): [number, number] => {
    const width = Math.max(0.001, bounds.maxX - bounds.minX);
    const height = Math.max(0.001, bounds.maxY - bounds.minY);
    return [60 + ((point.longitude - bounds.minX) / width) * 880, 560 - ((point.latitude - bounds.minY) / height) * 500];
  };
  const path = (geometry: GeoPoint[]) => renderGeometry(geometry).map((point, index) => {
    const [x, y] = project(point);
    return `${index ? "L" : "M"}${x.toFixed(1)},${y.toFixed(1)}`;
  }).join(" ");
  const start = selectedRoute?.geometry[0];
  const finish = selectedRoute?.geometry.at(-1);

  return (
    <svg className="schematic-map" viewBox="0 0 1000 620" preserveAspectRatio="xMidYMid slice" role="img" aria-label={label} aria-hidden={hidden} focusable="false">
      <defs>
        <pattern id="minor-grid" width="72" height="72" patternUnits="userSpaceOnUse" patternTransform="rotate(18)">
          <path d="M0 0V72M0 0H72" stroke="var(--map-grid)" strokeWidth="1" />
        </pattern>
        <filter id="route-shadow" x="-20%" y="-20%" width="140%" height="140%">
          <feDropShadow dx="0" dy="3" stdDeviation="5" floodColor="#21201f" floodOpacity=".18" />
        </filter>
      </defs>
      <rect width="1000" height="620" fill="var(--map-fallback-bg)" />
      <rect width="1000" height="620" fill="url(#minor-grid)" />
      <path d="M-30 500C190 355 245 420 420 295S740 170 1030 70" fill="none" stroke="var(--map-fallback-road-edge)" strokeWidth="86" />
      <path d="M-30 500C190 355 245 420 420 295S740 170 1030 70" fill="none" stroke="var(--map-fallback-road)" strokeWidth="28" />
      {routes.filter((route) => route.candidateId !== selectedRoute?.candidateId).map((route) => (
        <path key={route.candidateId} d={path(route.geometry)} fill="none" stroke="var(--map-route-alternative)" strokeWidth="3" strokeLinecap="round" />
      ))}
      {boundedItems(selectedRoute?.segments ?? [], MAX_RENDERED_SEGMENTS).map((segment) => (
        <path key={`${segment.segmentId}-outline`} d={path(segment.geometry)} fill="none" stroke="var(--map-route-casing)" strokeWidth="8" strokeLinecap="round" filter="url(#route-shadow)" />
      ))}
      {boundedItems(selectedRoute?.segments ?? [], MAX_RENDERED_SEGMENTS).map((segment) => (
        <path key={segment.segmentId} d={path(segment.geometry)} fill="none" stroke={segmentColor(segment)} strokeWidth="5" strokeLinecap="round" />
      ))}
      {start && <g transform={`translate(${project(start).join(" ")})`}><circle r="20" fill="var(--map-pin-start)" /><text fill="var(--map-pin-start-ink)" textAnchor="middle" dominantBaseline="central" fontSize="20" fontWeight="700">A</text></g>}
      {finish && <g transform={`translate(${project(finish).join(" ")})`}><circle r="20" fill="var(--accent)" stroke="var(--map-pin-start)" strokeWidth="5" /><text fill="#21201f" textAnchor="middle" dominantBaseline="central" fontSize="20" fontWeight="700">B</text></g>}
    </svg>
  );
}

function markerElement(label: string, kind: "start" | "finish"): HTMLElement {
  const element = document.createElement("div");
  element.className = `route-map-marker route-map-marker-${kind}`;
  element.textContent = label;
  element.setAttribute("aria-hidden", "true");
  return element;
}

// A route disappears into a map that is itself full of coloured roads. What
// makes it readable is contrast, not weight: every line is drawn twice, a thin
// dark casing first and the route colour on top, so the ribbon stays crisp and
// slim instead of blanketing the streets underneath.
const ROUTE_WIDTH = { alternativeCasing: 5, alternative: 3, selectedCasing: 6, selected: 4 } as const;

function routeCasingColor(theme: "light" | "dark"): string {
  return theme === "dark" ? "rgba(6, 6, 8, 0.85)" : "rgba(20, 19, 18, 0.55)";
}

function alternativeColor(theme: "light" | "dark"): string {
  return theme === "dark" ? "rgba(228, 227, 223, 0.62)" : "rgba(60, 58, 55, 0.62)";
}

function lineFeature(api: Ymaps3, points: GeoPoint[], color: string, width: number): MapEntity {
  return new api.YMapFeature({
    geometry: { type: "LineString", coordinates: yandexCoordinates(points) },
    style: { simplificationRate: 0, stroke: [{ color, width }] },
  });
}

function renderYandexRoutes(runtime: YandexRuntime, routes: RouteCandidate[], selectedRoute: RouteCandidate | undefined, theme: "light" | "dark"): MapEntity[] {
  const rendered: MapEntity[] = [];
  const add = (entity: MapEntity) => {
    runtime.map.addChild(entity);
    rendered.push(entity);
  };

  for (const route of routes.filter((item) => item.candidateId !== selectedRoute?.candidateId)) {
    add(lineFeature(runtime.api, route.geometry, routeCasingColor(theme), ROUTE_WIDTH.alternativeCasing));
    add(lineFeature(runtime.api, route.geometry, alternativeColor(theme), ROUTE_WIDTH.alternative));
  }
  if (selectedRoute) {
    add(lineFeature(runtime.api, selectedRoute.geometry, routeCasingColor(theme), ROUTE_WIDTH.selectedCasing));
    const segments = boundedItems(selectedRoute.segments, MAX_RENDERED_SEGMENTS);
    if (segments.length === 0) add(lineFeature(runtime.api, selectedRoute.geometry, "#1f7a4d", ROUTE_WIDTH.selected));
    for (const segment of segments) add(lineFeature(runtime.api, segment.geometry, segmentColor(segment), ROUTE_WIDTH.selected));

    const start = selectedRoute.geometry[0];
    const finish = selectedRoute.geometry.at(-1);
    if (start) add(new runtime.api.YMapMarker(
      { coordinates: [start.longitude, start.latitude], zIndex: 2000 },
      markerElement("A", "start"),
    ));
    if (finish) add(new runtime.api.YMapMarker(
      { coordinates: [finish.longitude, finish.latitude], zIndex: 2000 },
      markerElement("B", "finish"),
    ));
  }
  return rendered;
}

function renderTwoGisRoutes(runtime: TwoGisRuntime, routes: RouteCandidate[], selectedRoute: RouteCandidate | undefined, theme: "light" | "dark"): TwoGisObject[] {
  const rendered: TwoGisObject[] = [];
  const addLine = (points: GeoPoint[], color: string, width: number, zIndex: number) => {
    rendered.push(new runtime.api.Polyline(runtime.map, {
      color,
      coordinates: yandexCoordinates(points),
      renderingMode: "2d",
      width,
      zIndex,
    }));
  };

  for (const route of routes.filter((item) => item.candidateId !== selectedRoute?.candidateId)) {
    addLine(route.geometry, routeCasingColor(theme), ROUTE_WIDTH.alternativeCasing, 10);
    addLine(route.geometry, alternativeColor(theme), ROUTE_WIDTH.alternative, 11);
  }
  if (selectedRoute) {
    addLine(selectedRoute.geometry, routeCasingColor(theme), ROUTE_WIDTH.selectedCasing, 20);
    const segments = boundedItems(selectedRoute.segments, MAX_RENDERED_SEGMENTS);
    if (segments.length === 0) addLine(selectedRoute.geometry, "#1f7a4d", ROUTE_WIDTH.selected, 21);
    for (const segment of segments) addLine(segment.geometry, segmentColor(segment), ROUTE_WIDTH.selected, 21);

    const start = selectedRoute.geometry[0];
    const finish = selectedRoute.geometry.at(-1);
    if (start) rendered.push(new runtime.api.HtmlMarker(runtime.map, {
      coordinates: [start.longitude, start.latitude],
      html: markerElement("A", "start"),
      zIndex: 30,
    }));
    if (finish) rendered.push(new runtime.api.HtmlMarker(runtime.map, {
      coordinates: [finish.longitude, finish.latitude],
      html: markerElement("B", "finish"),
      zIndex: 30,
    }));
  }
  return rendered;
}

function safelyRemoveYandex(map: YandexMap3, entity: MapEntity): void {
  try {
    map.removeChild(entity);
  } catch {
    // The map may already be destroyed while an async loader is settling.
  }
}

function safelyDestroy(entity: TwoGisObject | undefined): void {
  try {
    entity?.destroy();
  } catch {
    // Dynamic objects may already be gone after the renderer is destroyed.
  }
}

function clearRuntimeRoutes(runtime: MapRuntime): void {
  if (runtime.kind === "yandex") {
    for (const entity of runtime.routeEntities) safelyRemoveYandex(runtime.map, entity);
  } else {
    for (const entity of runtime.routeEntities) safelyDestroy(entity);
  }
  runtime.routeEntities = [];
}

function clearYandexTraffic(runtime: YandexRuntime): void {
  for (const entity of runtime.trafficEntities) safelyRemoveYandex(runtime.map, entity);
  runtime.trafficEntities = [];
}

function destroyRuntime(runtime: MapRuntime): void {
  clearRuntimeRoutes(runtime);
  if (runtime.kind === "yandex") {
    clearYandexTraffic(runtime);
    if (runtime.userLocationMarker) safelyRemoveYandex(runtime.map, runtime.userLocationMarker);
  } else {
    safelyDestroy(runtime.userLocationMarker);
  }
  runtime.map.destroy();
}

type Props = {
  result?: RouteSearchResult | undefined;
  routingMode?: RoutingMode | undefined;
  selectedRoute?: RouteCandidate | undefined;
  // A route picked from the green podium. It is a preview of what the search
  // found, so it renders even in strict mode where nothing is selectable.
  previewRoute?: RouteCandidate | undefined;
  trafficProvider?: TrafficProvider | undefined;
  onTrafficProviderChange?: ((provider: TrafficProvider) => void) | undefined;
  browserKey?: string | undefined;
  twoGisBrowserKey?: string | undefined;
  // MapGL ships no dark scheme of its own; a dark map is a style created in the
  // 2GIS Style Editor and referenced by id.
  twoGisDarkStyleId?: string | undefined;
  yandexTrafficAvailable?: boolean | undefined;
  demoMode?: boolean | undefined;
};

export function RouteMap({
  result,
  routingMode,
  selectedRoute,
  previewRoute,
  trafficProvider,
  onTrafficProviderChange,
  browserKey,
  twoGisBrowserKey,
  twoGisDarkStyleId,
  yandexTrafficAvailable = false,
  demoMode = false,
}: Props) {
  const { locale, t } = useLocale();
  const theme = useResolvedTheme();
  const containerRef = useRef<HTMLDivElement>(null);
  const stageRef = useRef<HTMLElement>(null);
  const runtimeRef = useRef<MapRuntime | undefined>(undefined);
  const [runtimeGeneration, setRuntimeGeneration] = useState(0);
  const [status, setStatus] = useState<"fallback" | "loading" | "ready" | "error">(browserKey ? "loading" : "fallback");
  const [geolocationState, setGeolocationState] = useState<"idle" | "locating" | "error">("idle");
  const [fullscreen, setFullscreen] = useState(false);
  const [localTrafficProvider, setLocalTrafficProvider] = useState<TrafficProvider>("off");
  const [trafficError, setTrafficError] = useState(false);
  const [retryGeneration, setRetryGeneration] = useState(0);
  const trafficRenderer = trafficProvider ?? localTrafficProvider;
  const selectTrafficProvider = (provider: TrafficProvider) => {
    setTrafficError(false);
    if (trafficProvider === undefined) setLocalTrafficProvider(provider);
    onTrafficProviderChange?.(provider);
  };
  const activeRenderer: Exclude<TrafficRenderer, "yandex"> = trafficRenderer === "2gis" && twoGisBrowserKey ? "2gis" : "off";
  const mapConfigured = activeRenderer === "2gis" ? Boolean(twoGisBrowserKey) : Boolean(browserKey);
  const routes = useMemo(() => result ? routeCandidatesForMode(result, routingMode) : [], [result, routingMode]);
  const strictSelected = routingMode === "STRICT_GREEN" && !isVerifiedAllGreenRoute(selectedRoute) ? undefined : selectedRoute;
  const visibleSelectedRoute = previewRoute ?? strictSelected;
  const visibleRoutes = useMemo(
    () => renderedRoutes(previewRoute ? [previewRoute, ...routes] : routes, visibleSelectedRoute),
    [previewRoute, routes, visibleSelectedRoute],
  );
  useEffect(() => {
    window.queueMicrotask(() => setTrafficError(false));
  }, [trafficRenderer]);
  const labels = locale === "ru" ? {
    controls: "Управление картой",
    fullscreen: fullscreen ? "Выйти из полноэкранного режима" : "Открыть карту на весь экран",
    geolocation: "Показать моё местоположение",
    geolocationError: "Не удалось определить местоположение",
    traffic: "Пробки",
    trafficOff: "Выкл",
    trafficYandex: "Яндекс",
    trafficTwoGis: "2ГИС",
    trafficYandexUnavailable: "Нужен платный тариф",
    trafficYandexError: "Не удалось включить пробки Яндекса",
    trafficTwoGisUnavailable: "Нужен ключ 2GIS MapGL",
    twoGisMapError: "Проверьте ключ 2GIS MapGL. Маршруты пока показаны схематично.",
    retryMap: "Повторить",
    zoomIn: "Увеличить масштаб",
    zoomOut: "Уменьшить масштаб",
  } : {
    controls: "Map controls",
    fullscreen: fullscreen ? "Exit fullscreen" : "Open map fullscreen",
    geolocation: "Show my location",
    geolocationError: "Unable to determine location",
    traffic: "Traffic",
    trafficOff: "Off",
    trafficYandex: "Yandex",
    trafficTwoGis: "2GIS",
    trafficYandexUnavailable: "Paid plan required",
    trafficYandexError: "Yandex traffic could not be enabled",
    trafficTwoGisUnavailable: "2GIS MapGL key required",
    twoGisMapError: "Check the 2GIS MapGL key. Routes are shown schematically for now.",
    retryMap: "Retry",
    zoomIn: "Zoom in",
    zoomOut: "Zoom out",
  };

  useEffect(() => {
    const container = containerRef.current;
    const configured = activeRenderer === "2gis" ? Boolean(twoGisBrowserKey) : Boolean(browserKey);
    if (!configured || !container) {
      runtimeRef.current = undefined;
      setStatus("fallback");
      return;
    }

    let cancelled = false;
    let createdRuntime: MapRuntime | undefined;
    setStatus("loading");
    const initialize = async (): Promise<MapRuntime> => {
      if (activeRenderer === "2gis") {
        const api = await loadTwoGisMapGL();
        if (api.isSupported && !api.isSupported()) throw new Error("TWOGIS_MAP_UNSUPPORTED");
        if (cancelled) throw new Error("MAP_INITIALIZATION_CANCELLED");
        container.replaceChildren();
        const map = new api.Map(container, {
          center: DEFAULT_CENTER,
          controlsLayoutPadding: { top: 56, right: 72, bottom: 84, left: 56 },
          disableDragging: false,
          disablePitchByUserInteraction: true,
          disableRotationByUserInteraction: true,
          disableZoomOnScroll: false,
          enableTrackResize: true,
          key: twoGisBrowserKey,
          lang: locale,
          maxZoom: 20,
          minZoom: 4,
          ...(theme === "dark" && twoGisDarkStyleId ? { style: twoGisDarkStyleId } : {}),
          trafficControl: false,
          trafficOn: true,
          zoom: DEFAULT_ZOOM,
          zoomControl: false,
        });
        return { api, kind: "2gis", map, routeEntities: [] };
      }

      const api = await loadYandexMaps(browserKey!, locale);
      if (cancelled) throw new Error("MAP_INITIALIZATION_CANCELLED");
      container.replaceChildren();
      const map = new api.YMap(container, {
        behaviors: ["drag", "scrollZoom", "pinchZoom", "dblClick"],
        location: { center: DEFAULT_CENTER, zoom: DEFAULT_ZOOM },
        margin: [56, 72, 84, 56],
        mode: "auto",
        theme,
        zoomRange: { min: 4, max: 20 },
      });
      map
        .addChild(new api.YMapDefaultSchemeLayer({ theme }))
        .addChild(new api.YMapDefaultFeaturesLayer({ zIndex: 1800 }));
      return { api, kind: "yandex", map, routeEntities: [], trafficEntities: [] };
    };

    void initialize().then((runtime) => {
      if (cancelled) {
        destroyRuntime(runtime);
        return;
      }
      createdRuntime = runtime;
      runtimeRef.current = runtime;
      setRuntimeGeneration((generation) => generation + 1);
      setStatus("ready");
    }).catch(() => {
      if (!cancelled) {
        if (createdRuntime) destroyRuntime(createdRuntime);
        container.replaceChildren();
        setStatus("error");
      }
    });

    return () => {
      cancelled = true;
      if (createdRuntime) {
        if (runtimeRef.current === createdRuntime) runtimeRef.current = undefined;
        destroyRuntime(createdRuntime);
        container.replaceChildren();
      }
    };
  }, [activeRenderer, browserKey, locale, retryGeneration, theme, twoGisBrowserKey, twoGisDarkStyleId]);

  useEffect(() => {
    const runtime = runtimeRef.current;
    if (!runtime) return;

    clearRuntimeRoutes(runtime);
    try {
      if (runtime.kind === "yandex") runtime.routeEntities = renderYandexRoutes(runtime, visibleRoutes, visibleSelectedRoute, theme);
      else runtime.routeEntities = renderTwoGisRoutes(runtime, visibleRoutes, visibleSelectedRoute, theme);
      const bounds = routeBounds(visibleRoutes);
      if (bounds) {
        if (runtime.kind === "yandex") {
          runtime.map.setLocation({ bounds, duration: 240, easing: "ease-in-out" });
        } else {
          runtime.map.fitBounds({ southWest: bounds[0], northEast: bounds[1] }, {
            padding: { top: 56, right: 72, bottom: 84, left: 56 },
          });
        }
      }
      window.queueMicrotask(() => {
        if (runtimeRef.current === runtime) setStatus("ready");
      });
    } catch {
      clearRuntimeRoutes(runtime);
      window.queueMicrotask(() => {
        if (runtimeRef.current === runtime) setStatus("error");
      });
    }

    return () => {
      clearRuntimeRoutes(runtime);
    };
  }, [runtimeGeneration, visibleRoutes, visibleSelectedRoute, theme]);

  useEffect(() => {
    const runtime = runtimeRef.current;
    if (!runtime || runtime.kind !== "yandex") return;

    clearYandexTraffic(runtime);
    if (trafficRenderer !== "yandex") return;
    if (!yandexTrafficAvailable) {
      window.queueMicrotask(() => setTrafficError(true));
      return;
    }

    let cancelled = false;
    void runtime.api.import("@yandex/ymaps3-layers-extra").then((trafficModule) => {
      if (cancelled || runtimeRef.current !== runtime || trafficRenderer !== "yandex") return;
      const TrafficLayer = trafficModule.YMapTrafficLayer as (new (props: Record<string, unknown>) => MapEntity) | undefined;
      const TrafficEventsLayer = trafficModule.YMapTrafficEventsLayer as (new (props: Record<string, unknown>) => MapEntity) | undefined;
      if (typeof TrafficLayer !== "function" || typeof TrafficEventsLayer !== "function") {
        throw new Error("YANDEX_TRAFFIC_MODULE_INVALID");
      }

      const attached: MapEntity[] = [];
      try {
        const trafficLayer = new TrafficLayer({ visible: true });
        runtime.map.addChild(trafficLayer);
        attached.push(trafficLayer);
        const trafficEventsLayer = new TrafficEventsLayer({ visible: true });
        runtime.map.addChild(trafficEventsLayer);
        attached.push(trafficEventsLayer);
        runtime.trafficEntities = attached;
      } catch (error) {
        for (const entity of attached) safelyRemoveYandex(runtime.map, entity);
        throw error;
      }
    }).catch(() => {
      if (cancelled || runtimeRef.current !== runtime) return;
      clearYandexTraffic(runtime);
      setTrafficError(true);
    });

    return () => {
      cancelled = true;
      clearYandexTraffic(runtime);
    };
  }, [runtimeGeneration, trafficRenderer, yandexTrafficAvailable]);

  useEffect(() => {
    const onFullscreenChange = () => setFullscreen(document.fullscreenElement === stageRef.current);
    document.addEventListener("fullscreenchange", onFullscreenChange);
    return () => document.removeEventListener("fullscreenchange", onFullscreenChange);
  }, []);

  const changeZoom = (delta: number) => {
    const runtime = runtimeRef.current;
    if (!runtime || status !== "ready") return;
    if (runtime.kind === "yandex") {
      const current = Number.isFinite(runtime.map.zoom) ? runtime.map.zoom : DEFAULT_ZOOM;
      runtime.map.setLocation({ zoom: Math.max(4, Math.min(20, current + delta)), duration: 180, easing: "ease-out" });
    } else {
      const current = runtime.map.getZoom();
      runtime.map.setZoom(Math.max(4, Math.min(20, current + delta)), { duration: 180 });
    }
  };

  const showCurrentLocation = () => {
    const runtime = runtimeRef.current;
    if (!runtime || status !== "ready" || !("geolocation" in navigator)) {
      setGeolocationState("error");
      return;
    }
    setGeolocationState("locating");
    navigator.geolocation.getCurrentPosition((position) => {
      if (runtimeRef.current !== runtime) return;
      try {
        const coordinates: LngLat = [position.coords.longitude, position.coords.latitude];
        const element = document.createElement("div");
        element.className = "route-map-user-location";
        element.setAttribute("aria-hidden", "true");
        if (runtime.kind === "yandex") {
          if (runtime.userLocationMarker) safelyRemoveYandex(runtime.map, runtime.userLocationMarker);
          runtime.userLocationMarker = new runtime.api.YMapMarker({ coordinates, zIndex: 2100 }, element);
          runtime.map.addChild(runtime.userLocationMarker);
          runtime.map.setLocation({ center: coordinates, zoom: 16, duration: 300, easing: "ease-in-out" });
        } else {
          safelyDestroy(runtime.userLocationMarker);
          runtime.userLocationMarker = new runtime.api.HtmlMarker(runtime.map, { coordinates, html: element, zIndex: 2100 });
          runtime.map.setCenter(coordinates, { duration: 300 });
          runtime.map.setZoom(16, { duration: 300 });
        }
        setGeolocationState("idle");
      } catch {
        setGeolocationState("error");
      }
    }, () => {
      if (runtimeRef.current === runtime) setGeolocationState("error");
    }, {
      enableHighAccuracy: false,
      maximumAge: 60_000,
      timeout: 8_000,
    });
  };

  const toggleFullscreen = () => {
    if (document.fullscreenElement) {
      void document.exitFullscreen?.().catch(() => undefined);
    } else {
      void stageRef.current?.requestFullscreen?.().catch(() => undefined);
    }
  };

  return (
    <section ref={stageRef} className="map-stage" aria-label={t("mapRegion")}>
      {/* Real streets from OpenStreetMap when there is no provider key; the
          abstract grid only remains for the moment before the map has size. */}
      {status === "ready" ? (
        <SchematicMap routes={visibleRoutes} selectedRoute={visibleSelectedRoute} label={t("mapFallbackTitle")} hidden />
      ) : (
        <TileMap
          routes={visibleRoutes}
          selectedRoute={visibleSelectedRoute}
          segmentColor={segmentColor}
          label={t("mapFallbackTitle")}
        />
      )}
      <div
        ref={containerRef}
        className={`map-canvas map-renderer-${activeRenderer} ${status === "ready" ? "is-ready" : ""}`}
        aria-hidden={status !== "ready"}
        data-map-renderer={activeRenderer === "2gis" ? "2gis" : "yandex"}
      />

      <div className="traffic-renderer-picker">
        <span className="traffic-renderer-title" id="traffic-renderer-label" title={labels.traffic}>
          <svg aria-hidden="true" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8"><path d="M5 18h14M7 14h10M9 10h6M11 6h2" /></svg>
          <span className="sr-only">{labels.traffic}</span>
        </span>
        <div className="traffic-renderer-segments" role="radiogroup" aria-labelledby="traffic-renderer-label">
          <button type="button" role="radio" aria-checked={trafficRenderer === "off"} onClick={() => selectTrafficProvider("off")}>
            {labels.trafficOff}
          </button>
          <button
            type="button"
            role="radio"
            aria-checked={trafficRenderer === "yandex"}
            aria-disabled={!yandexTrafficAvailable}
            aria-label={yandexTrafficAvailable ? labels.trafficYandex : `${labels.trafficYandex}. ${labels.trafficYandexUnavailable}`}
            title={yandexTrafficAvailable ? labels.trafficYandex : labels.trafficYandexUnavailable}
            onClick={() => { if (yandexTrafficAvailable) selectTrafficProvider("yandex"); }}
          >
            {labels.trafficYandex}
            {!yandexTrafficAvailable && <span className="traffic-renderer-reason" aria-hidden="true">{labels.trafficYandexUnavailable}</span>}
          </button>
          <button
            type="button"
            role="radio"
            aria-checked={trafficRenderer === "2gis"}
            aria-disabled={!twoGisBrowserKey}
            aria-label={twoGisBrowserKey ? labels.trafficTwoGis : `${labels.trafficTwoGis}. ${labels.trafficTwoGisUnavailable}`}
            title={twoGisBrowserKey ? labels.trafficTwoGis : labels.trafficTwoGisUnavailable}
            onClick={() => { if (twoGisBrowserKey) selectTrafficProvider("2gis"); }}
          >
            {labels.trafficTwoGis}
            {!twoGisBrowserKey && <span className="traffic-renderer-reason" aria-hidden="true">{labels.trafficTwoGisUnavailable}</span>}
          </button>
        </div>
      </div>
      {trafficError && <div className="traffic-renderer-alert" role="status">{labels.trafficYandexError}</div>}

      {mapConfigured && (
        <div className="route-map-controls" role="group" aria-label={labels.controls}>
          <div className="route-map-control-group">
            <button type="button" onClick={() => changeZoom(1)} disabled={status !== "ready"} aria-label={labels.zoomIn} title={labels.zoomIn}>+</button>
            <button type="button" onClick={() => changeZoom(-1)} disabled={status !== "ready"} aria-label={labels.zoomOut} title={labels.zoomOut}>−</button>
          </div>
          <button
            type="button"
            onClick={showCurrentLocation}
            disabled={status !== "ready" || geolocationState === "locating"}
            aria-busy={geolocationState === "locating"}
            aria-label={labels.geolocation}
            title={labels.geolocation}
          >
            <CrosshairIcon />
          </button>
          <button type="button" onClick={toggleFullscreen} disabled={status !== "ready"} aria-pressed={fullscreen} aria-label={labels.fullscreen} title={labels.fullscreen}>
            <svg aria-hidden="true" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8"><path d="M8 3H3v5M16 3h5v5M8 21H3v-5M16 21h5v-5" /></svg>
          </button>
        </div>
      )}
      {geolocationState === "error" && <span className="sr-only" role="status">{labels.geolocationError}</span>}

      {(status === "loading" || status === "error" || status === "fallback") && (
        <div className={`map-status map-status-${status}`} role="status">
          <span className="map-status-dot" aria-hidden="true" />
          <span className="map-status-text">
            <strong>{status === "loading" ? t("mapLoading") : status === "error" ? t("mapError") : t("mapFallbackTitle")}</strong>
            {status !== "loading" && (
              <small>
                {status === "error" && activeRenderer === "2gis"
                  ? labels.twoGisMapError
                  /* The demo ships no key on purpose; "check your key" would read
                     as a broken deployment rather than a deliberate one. */
                  : demoMode ? t("mapFallbackDemo") : t("mapFallbackBody")}
              </small>
            )}
          </span>
          {status === "error" && <button type="button" className="map-retry" onClick={() => setRetryGeneration((generation) => generation + 1)}>{labels.retryMap}</button>}
        </div>
      )}
    </section>
  );
}
