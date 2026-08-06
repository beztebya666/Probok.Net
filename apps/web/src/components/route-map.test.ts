import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { createElement } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { createDemoSearch, getDemoSearch } from "@/lib/demo-fixtures";
import { LocaleProvider } from "@/lib/i18n";
import type { RouteSearchRequest } from "@/lib/schemas";
import { loadTwoGisMapGL, loadYandexMaps, RouteMap } from "./route-map";

type LocationRequest = { bounds?: [[number, number], [number, number]]; center?: [number, number]; zoom?: number };

let latestMap: FakeMap | undefined;
let mapProps: Record<string, unknown> | undefined;
let latestTwoGisMap: FakeTwoGisMap | undefined;
let twoGisMapProps: Record<string, unknown> | undefined;

function rememberMap(map: FakeMap) {
  latestMap = map;
}

function rememberTwoGisMap(map: FakeTwoGisMap) {
  latestTwoGisMap = map;
}

class FakeMap {
  readonly children: unknown[] = [];
  readonly removed: unknown[] = [];
  readonly locations: LocationRequest[] = [];
  destroyed = false;
  zoom = 11;

  constructor(_container: HTMLElement, props: Record<string, unknown>) {
    mapProps = props;
    rememberMap(this);
  }

  addChild(entity: unknown) {
    this.children.push(entity);
    return this;
  }

  removeChild(entity: unknown) {
    this.removed.push(entity);
    const index = this.children.indexOf(entity);
    if (index >= 0) this.children.splice(index, 1);
    return this;
  }

  setLocation(location: LocationRequest) {
    this.locations.push(location);
    if (typeof location.zoom === "number") this.zoom = location.zoom;
  }

  destroy() { this.destroyed = true; }
}

class FakeSchemeLayer {
  readonly kind = "scheme";
  constructor(readonly props: Record<string, unknown> = {}) {}
}

class FakeFeaturesLayer {
  readonly kind = "features";
  constructor(readonly props: Record<string, unknown> = {}) {}
}

class FakeFeature {
  readonly kind = "feature";
  constructor(readonly props: Record<string, unknown>) {}
}

class FakeMarker {
  readonly kind = "marker";
  constructor(readonly props: Record<string, unknown>, readonly element?: HTMLElement) {}
}

class FakeTrafficLayer {
  readonly kind = "traffic";
  constructor(readonly props: Record<string, unknown>) {}
}

class FakeTrafficEventsLayer {
  readonly kind = "traffic-events";
  constructor(readonly props: Record<string, unknown>) {}
}

function installAPI(importer = vi.fn().mockResolvedValue({
  YMapTrafficLayer: FakeTrafficLayer,
  YMapTrafficEventsLayer: FakeTrafficEventsLayer,
})) {
  const api = {
    import: importer,
    ready: Promise.resolve(),
    YMap: FakeMap,
    YMapDefaultSchemeLayer: FakeSchemeLayer,
    YMapDefaultFeaturesLayer: FakeFeaturesLayer,
    YMapFeature: FakeFeature,
    YMapMarker: FakeMarker,
  };
  Object.defineProperty(window, "ymaps3", { configurable: true, value: api });
  return api;
}

class FakeTwoGisMap {
  readonly objects: FakeTwoGisObject[] = [];
  readonly bounds: Array<{ northEast: [number, number]; southWest: [number, number] }> = [];
  readonly centers: Array<[number, number]> = [];
  readonly zooms: number[] = [];
  destroyed = false;
  zoom = 11;

  constructor(_container: HTMLElement | string, props: Record<string, unknown>) {
    twoGisMapProps = props;
    rememberTwoGisMap(this);
  }

  destroy() { this.destroyed = true; }
  fitBounds(bounds: { northEast: [number, number]; southWest: [number, number] }) { this.bounds.push(bounds); }
  getZoom() { return this.zoom; }
  setCenter(center: [number, number]) { this.centers.push(center); }
  setZoom(zoom: number) { this.zoom = zoom; this.zooms.push(zoom); }
}

class FakeTwoGisObject {
  destroyed = false;
  constructor(readonly map: FakeTwoGisMap, readonly props: Record<string, unknown>) { map.objects.push(this); }
  destroy() { this.destroyed = true; }
}

class FakeTwoGisPolyline extends FakeTwoGisObject {
  readonly kind = "polyline";
}

class FakeTwoGisHtmlMarker extends FakeTwoGisObject {
  readonly kind = "html-marker";
}

function installTwoGisAPI() {
  const api = {
    isSupported: () => true,
    Map: FakeTwoGisMap,
    Polyline: FakeTwoGisPolyline,
    HtmlMarker: FakeTwoGisHtmlMarker,
  };
  Object.defineProperty(window, "mapgl", { configurable: true, value: api });
  return api;
}

const request: RouteSearchRequest = {
  requestId: "f595719d-ee51-454d-ae16-9c502416df3d",
  origin: { latitude: 55.75222, longitude: 37.61556 },
  destination: { latitude: 55.8299, longitude: 37.6331 },
  waypoints: [],
  departureTime: "2026-08-05T12:00:00.000Z",
  routingMode: "GREENEST",
  maxExtraDistanceMeters: 30_000,
  maxExtraDistancePercent: 100,
  maxExtraTimeSeconds: 1_200,
  avoidTolls: false,
  avoidUnpaved: true,
  strictness: 0.82,
  maxProviderRequests: 4,
  searchDeadlineMs: 20_000,
};

const originalGeolocation = Object.getOwnPropertyDescriptor(navigator, "geolocation");
const originalRequestFullscreen = Object.getOwnPropertyDescriptor(HTMLElement.prototype, "requestFullscreen");

afterEach(() => {
  cleanup();
  document.head.querySelectorAll<HTMLScriptElement>('script[src^="https://api-maps.yandex.ru/"]').forEach((script) => script.remove());
  document.head.querySelectorAll<HTMLScriptElement>('script[src^="https://mapgl.2gis.com/"]').forEach((script) => script.remove());
  Reflect.deleteProperty(window, "mapgl");
  Reflect.deleteProperty(window, "ymaps3");
  latestMap = undefined;
  mapProps = undefined;
  latestTwoGisMap = undefined;
  twoGisMapProps = undefined;
  if (originalGeolocation) Object.defineProperty(navigator, "geolocation", originalGeolocation);
  else Reflect.deleteProperty(navigator, "geolocation");
  if (originalRequestFullscreen) Object.defineProperty(HTMLElement.prototype, "requestFullscreen", originalRequestFullscreen);
  else Reflect.deleteProperty(HTMLElement.prototype, "requestFullscreen");
});

describe("Yandex Maps v3 loader", () => {
  it("loads the official v3 script, awaits ymaps3.ready and removes the key-bearing script", async () => {
    const loading = loadYandexMaps("local-browser-key", "ru");
    const script = document.head.querySelector<HTMLScriptElement>('script[src^="https://api-maps.yandex.ru/v3/"]');
    expect(script).not.toBeNull();
    const url = new URL(script!.src);
    expect(url.pathname).toBe("/v3/");
    expect(url.searchParams.get("apikey")).toBe("local-browser-key");
    expect(url.searchParams.get("lang")).toBe("ru_RU");
    expect(script?.hasAttribute("crossorigin")).toBe(false);
    expect(script?.referrerPolicy).toBe("strict-origin-when-cross-origin");

    const api = installAPI();
    script?.dispatchEvent(new Event("load"));

    await expect(loading).resolves.toBe(api);
    expect(document.head.contains(script)).toBe(false);
  });

  it("returns a key-free stable error and removes the script after a load failure", async () => {
    const loading = loadYandexMaps("never-expose-this-key", "en");
    const assertion = expect(loading).rejects.toThrow("YANDEX_MAP_LOAD_FAILED");
    const script = document.head.querySelector<HTMLScriptElement>('script[src^="https://api-maps.yandex.ru/v3/"]');
    script?.dispatchEvent(new Event("error"));

    await assertion;
    expect(document.documentElement.outerHTML).not.toContain("never-expose-this-key");
  });
});

describe("2GIS MapGL loader", () => {
  it("loads the official key-free CDN script and removes it after initialization", async () => {
    const loading = loadTwoGisMapGL();
    const script = document.head.querySelector<HTMLScriptElement>('script[src^="https://mapgl.2gis.com/"]');
    expect(script).not.toBeNull();
    const url = new URL(script!.src);
    expect(url.origin).toBe("https://mapgl.2gis.com");
    expect(url.pathname).toBe("/api/js/v1");
    expect(url.search).toBe("");
    expect(script?.hasAttribute("crossorigin")).toBe(false);
    expect(script?.referrerPolicy).toBe("strict-origin-when-cross-origin");

    const api = installTwoGisAPI();
    script?.dispatchEvent(new Event("load"));

    await expect(loading).resolves.toBe(api);
    expect(document.head.contains(script)).toBe(false);
  });

  it("returns a stable error and leaves no loader script after a failure", async () => {
    const loading = loadTwoGisMapGL();
    const assertion = expect(loading).rejects.toThrow("TWOGIS_MAP_LOAD_FAILED");
    const script = document.head.querySelector<HTMLScriptElement>('script[src^="https://mapgl.2gis.com/"]');
    script?.dispatchEvent(new Event("error"));

    await assertion;
    expect(document.head.contains(script)).toBe(false);
  });
});

describe("RouteMap v3 integration", () => {
  it("renders v3 layers, longitude-first route features and markers, then fits bounds", async () => {
    installAPI();
    createDemoSearch(request);
    const result = getDemoSearch(`demo-${request.requestId}`, true);
    const selected = result.selectedRoute ?? undefined;
    const view = render(createElement(LocaleProvider, null, createElement(RouteMap, {
      browserKey: "local-browser-key",
      result,
      selectedRoute: selected,
    })));

    await waitFor(() => expect(latestMap?.children.some((entity) => entity instanceof FakeFeature)).toBe(true));
    expect(mapProps?.behaviors).toEqual(["drag", "scrollZoom", "pinchZoom", "dblClick"]);
    expect(latestMap?.children.some((entity) => entity instanceof FakeSchemeLayer)).toBe(true);
    expect(latestMap?.children.some((entity) => entity instanceof FakeFeaturesLayer)).toBe(true);

    const line = latestMap?.children.find((entity): entity is FakeFeature => entity instanceof FakeFeature);
    const geometry = line?.props.geometry as { coordinates: Array<[number, number]> };
    expect(geometry.coordinates[0]).toEqual([expect.any(Number), expect.any(Number)]);
    expect(geometry.coordinates[0]![0]).toBeGreaterThan(30);
    expect(geometry.coordinates[0]![1]).toBeGreaterThan(50);
    expect(latestMap?.children.filter((entity) => entity instanceof FakeMarker)).toHaveLength(2);
    const routeWidths = latestMap?.children
      .filter((entity): entity is FakeFeature => entity instanceof FakeFeature)
      .map((feature) => ((feature.props.style as { stroke?: Array<{ width: number }> } | undefined)?.stroke?.[0]?.width))
      .filter((width): width is number => typeof width === "number") ?? [];
    // Every line is drawn as a casing plus the colour on top, so the widest
    // stroke belongs to the selected route's casing and the visible colours
    // stay slimmer than it.
    expect(Math.max(...routeWidths)).toBe(6);
    expect(routeWidths).toContain(4);
    expect(routeWidths).toContain(3);

    const fit = latestMap?.locations.find((location) => location.bounds);
    expect(fit?.bounds?.[0][0]).toBeLessThan(fit!.bounds![1][0]);
    expect(fit?.bounds?.[0][1]).toBeLessThan(fit!.bounds![1][1]);

    const previousFitCount = latestMap!.locations.length;
    const alternative = result.alternatives[0];
    view.rerender(createElement(LocaleProvider, null, createElement(RouteMap, {
      browserKey: "local-browser-key",
      result,
      selectedRoute: alternative,
    })));
    await waitFor(() => expect(latestMap!.locations.length).toBeGreaterThan(previousFitCount));
    expect(latestMap!.removed.length).toBeGreaterThan(0);
  });

  it("draws no provider reference route when strict green has no verified selection", async () => {
    installAPI();
    createDemoSearch(request);
    const result = getDemoSearch(`demo-${request.requestId}`, true);
    const reference = result.fastestReferenceRoute ?? undefined;
    result.selectedRoute = null;

    render(createElement(LocaleProvider, null, createElement(RouteMap, {
      browserKey: "local-browser-key",
      result,
      routingMode: "STRICT_GREEN",
      // Even a stale caller selection must not bypass the strict-mode guard.
      selectedRoute: reference,
    })));

    await waitFor(() => expect(latestMap).toBeDefined());
    expect(latestMap?.children.some((entity) => entity instanceof FakeFeature)).toBe(false);
    expect(latestMap?.children.some((entity) => entity instanceof FakeMarker)).toBe(false);
    expect(latestMap?.locations.some((location) => Boolean(location.bounds))).toBe(false);
  });

  it("provides accessible zoom, geolocation and fullscreen controls and destroys the map", async () => {
    installAPI();
    Object.defineProperty(navigator, "geolocation", {
      configurable: true,
      value: {
        getCurrentPosition: vi.fn((success: PositionCallback) => success({
          coords: { latitude: 55.7, longitude: 37.5 },
        } as GeolocationPosition)),
      },
    });
    const requestFullscreen = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(HTMLElement.prototype, "requestFullscreen", { configurable: true, value: requestFullscreen });
    const user = userEvent.setup();
    const view = render(createElement(LocaleProvider, null, createElement(RouteMap, { browserKey: "local-browser-key" })));

    const zoomIn = await screen.findByRole("button", { name: /Увеличить масштаб|Zoom in/ });
    await waitFor(() => expect(zoomIn).toBeEnabled());
    await user.click(zoomIn);
    expect(latestMap?.locations.at(-1)?.zoom).toBe(12);

    await user.click(screen.getByRole("button", { name: /Показать моё местоположение|Show my location/ }));
    expect(latestMap?.locations.at(-1)).toMatchObject({ center: [37.5, 55.7], zoom: 16 });
    expect(latestMap?.children.some((entity) => entity instanceof FakeMarker && entity.element?.className === "route-map-user-location")).toBe(true);

    await user.click(screen.getByRole("button", { name: /Открыть карту на весь экран|Open map fullscreen/ }));
    expect(requestFullscreen).toHaveBeenCalledOnce();

    const map = latestMap!;
    view.unmount();
    expect(map.destroyed).toBe(true);
  });

  it("starts with traffic off and explains unavailable providers without selecting them", async () => {
    installAPI();
    render(createElement(LocaleProvider, null, createElement(RouteMap, { browserKey: "local-browser-key" })));

    await waitFor(() => expect(latestMap).toBeDefined());
    const [off, yandex, twoGis] = screen.getAllByRole("radio");
    expect(off).toHaveAttribute("aria-checked", "true");
    expect(yandex).toHaveAttribute("aria-checked", "false");
    expect(yandex).toHaveAttribute("aria-disabled", "true");
    expect(yandex?.getAttribute("title")).toBeTruthy();
    expect(twoGis).toHaveAttribute("aria-checked", "false");
    expect(twoGis).toHaveAttribute("aria-disabled", "true");
    expect(twoGis?.getAttribute("title")).toBeTruthy();
  });

  it("loads both official Yandex traffic layers on demand and removes them when switched off", async () => {
    const importer = vi.fn().mockResolvedValue({
      YMapTrafficLayer: FakeTrafficLayer,
      YMapTrafficEventsLayer: FakeTrafficEventsLayer,
    });
    installAPI(importer);
    const user = userEvent.setup();
    render(createElement(LocaleProvider, null, createElement(RouteMap, {
      browserKey: "local-browser-key",
      yandexTrafficAvailable: true,
    })));

    await waitFor(() => expect(latestMap).toBeDefined());
    const [off, yandex] = screen.getAllByRole("radio");
    await user.click(yandex!);

    await waitFor(() => expect(importer).toHaveBeenCalledWith("@yandex/ymaps3-layers-extra"));
    await waitFor(() => {
      expect(latestMap?.children.some((entity) => entity instanceof FakeTrafficLayer)).toBe(true);
      expect(latestMap?.children.some((entity) => entity instanceof FakeTrafficEventsLayer)).toBe(true);
    });
    expect(latestMap?.children.find((entity) => entity instanceof FakeTrafficLayer)?.props).toEqual({ visible: true });
    expect(latestMap?.children.find((entity) => entity instanceof FakeTrafficEventsLayer)?.props).toEqual({ visible: true });
    expect(window.mapgl).toBeUndefined();

    await user.click(off!);
    await waitFor(() => {
      expect(latestMap?.children.some((entity) => entity instanceof FakeTrafficLayer)).toBe(false);
      expect(latestMap?.children.some((entity) => entity instanceof FakeTrafficEventsLayer)).toBe(false);
    });
  });

  it("keeps the selected provider with a key-free error when Yandex traffic fails", async () => {
    const browserKey = "never-render-this-browser-key";
    installAPI(vi.fn().mockRejectedValue(new Error(`provider failure: ${browserKey}`)));
    const user = userEvent.setup();
    render(createElement(LocaleProvider, null, createElement(RouteMap, {
      browserKey,
      yandexTrafficAvailable: true,
    })));

    await waitFor(() => expect(latestMap).toBeDefined());
    const [, yandex] = screen.getAllByRole("radio");
    await user.click(yandex!);

    await waitFor(() => expect(screen.getAllByRole("radio")[1]).toHaveAttribute("aria-checked", "true"));
    expect(screen.getAllByRole("radio")[0]).toHaveAttribute("aria-checked", "false");
    expect(screen.getByRole("status")).toBeInTheDocument();
    expect(document.body.textContent).not.toContain(browserKey);
    expect(document.body.textContent).not.toContain("provider failure");
    expect(latestMap?.destroyed).toBe(false);
  });

  it("keeps a controlled 2GIS selection on runtime failure and retries without changing preference", async () => {
    let attempts = 0;
    class FailingMap {
      constructor() {
        attempts += 1;
        throw new Error("runtime failed with secret details");
      }
    }
    Object.defineProperty(window, "mapgl", {
      configurable: true,
      value: { isSupported: () => true, Map: FailingMap, Polyline: FakeTwoGisPolyline, HtmlMarker: FakeTwoGisHtmlMarker },
    });
    const onTrafficProviderChange = vi.fn();
    const user = userEvent.setup();
    render(createElement(LocaleProvider, null, createElement(RouteMap, {
      browserKey: "local-browser-key",
      twoGisBrowserKey: "2gis-browser-key",
      trafficProvider: "2gis",
      onTrafficProviderChange,
    })));

    await screen.findByText(/Интерактивная карта недоступна|Interactive map unavailable/);
    expect(screen.getAllByRole("radio")[2]).toHaveAttribute("aria-checked", "true");
    expect(screen.getAllByRole("radio")[0]).toHaveAttribute("aria-checked", "false");
    expect(onTrafficProviderChange).not.toHaveBeenCalled();
    expect(document.body).not.toHaveTextContent("secret details");

    await user.click(screen.getByRole("button", { name: /Повторить|Retry/ }));
    await waitFor(() => expect(attempts).toBeGreaterThanOrEqual(2));
    expect(screen.getAllByRole("radio")[2]).toHaveAttribute("aria-checked", "true");
    expect(onTrafficProviderChange).not.toHaveBeenCalled();
  });

  it("fully replaces Yandex with an interactive 2GIS traffic map and restores it without mixing runtimes", async () => {
    installAPI();
    installTwoGisAPI();
    Object.defineProperty(navigator, "geolocation", {
      configurable: true,
      value: {
        getCurrentPosition: vi.fn((success: PositionCallback) => success({
          coords: { latitude: 55.7, longitude: 37.5 },
        } as GeolocationPosition)),
      },
    });
    createDemoSearch(request);
    const result = getDemoSearch(`demo-${request.requestId}`, true);
    const selected = result.selectedRoute ?? undefined;
    const user = userEvent.setup();
    const view = render(createElement(LocaleProvider, null, createElement(RouteMap, {
      browserKey: "local-browser-key",
      twoGisBrowserKey: "2gis-browser-key",
      result,
      selectedRoute: selected,
    })));

    await waitFor(() => expect(latestMap?.children.some((entity) => entity instanceof FakeFeature)).toBe(true));
    const originalYandexMap = latestMap!;
    await user.click(screen.getAllByRole("radio")[2]!);

    await waitFor(() => expect(latestTwoGisMap).toBeDefined());
    const twoGisMap = latestTwoGisMap!;
    await waitFor(() => expect(twoGisMap.objects.some((object) => object instanceof FakeTwoGisPolyline)).toBe(true));
    expect(originalYandexMap.destroyed).toBe(true);
    expect(twoGisMapProps).toMatchObject({
      disableDragging: false,
      disableZoomOnScroll: false,
      key: "2gis-browser-key",
      trafficControl: false,
      trafficOn: true,
      zoomControl: false,
    });
    expect(view.container.querySelector('[data-map-renderer="2gis"]')).toBeInTheDocument();
    expect(twoGisMap.objects.filter((object) => object instanceof FakeTwoGisPolyline && !object.destroyed).length).toBeGreaterThan(0);
    const twoGisWidths = twoGisMap.objects
      .filter((object): object is FakeTwoGisPolyline => object instanceof FakeTwoGisPolyline && !object.destroyed)
      .map((polyline) => polyline.props.width as number);
    expect(Math.max(...twoGisWidths)).toBe(6);
    expect(twoGisWidths).toContain(4);
    expect(twoGisWidths).toContain(3);
    expect(twoGisMap.objects.filter((object) => object instanceof FakeTwoGisHtmlMarker && !object.destroyed)).toHaveLength(2);
    expect(twoGisMap.bounds.length).toBeGreaterThan(0);
    expect(twoGisMap.bounds[0]!.southWest[0]).toBeLessThan(twoGisMap.bounds[0]!.northEast[0]);
    expect(twoGisMap.bounds[0]!.southWest[1]).toBeLessThan(twoGisMap.bounds[0]!.northEast[1]);

    const zoomIn = screen.getByRole("button", { name: /Увеличить масштаб|Zoom in/ });
    await user.click(zoomIn);
    expect(twoGisMap.zooms.at(-1)).toBe(12);
    await user.click(screen.getByRole("button", { name: /Показать моё местоположение|Show my location/ }));
    expect(twoGisMap.centers.at(-1)).toEqual([37.5, 55.7]);
    expect(twoGisMap.zooms.at(-1)).toBe(16);

    await user.click(screen.getAllByRole("radio")[0]!);
    await waitFor(() => expect(latestMap).not.toBe(originalYandexMap));
    await waitFor(() => expect(latestMap?.destroyed).toBe(false));
    expect(twoGisMap.destroyed).toBe(true);
    expect(twoGisMap.objects.every((object) => object.destroyed)).toBe(true);
    expect(view.container.querySelector('[data-map-renderer="yandex"]')).toBeInTheDocument();
  });
});
