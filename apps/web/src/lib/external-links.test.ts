import { describe, expect, it } from "vitest";
import { externalRoutePoints, twoGisRouteUrl, yandexRouteUrl } from "./external-links";
import type { GeoPoint } from "./schemas";

const geometry: GeoPoint[] = Array.from({ length: 40 }, (_, index) => ({
  latitude: 55.7 + index * 0.002,
  longitude: 37.6 + index * 0.003,
}));

describe("external route links", () => {
  it("never leaves an empty first slot that 2GIS would fill with the viewer location", () => {
    const url = twoGisRouteUrl({ geometry })!;
    const path = decodeURIComponent(new URL(url).pathname).replace("/directions/points/", "");
    expect(path.startsWith("|")).toBe(false);
    expect(path.split("|").every((pair) => /^-?\d/.test(pair))).toBe(true);
  });

  it("keeps both endpoints when reducing a long geometry to a link-sized list", () => {
    const points = externalRoutePoints(geometry);
    expect(points.length).toBeLessThanOrEqual(10);
    expect(points[0]).toEqual(geometry[0]);
    expect(points.at(-1)).toEqual(geometry.at(-1));
  });

  it("spends its via points on the turns rather than on straight stretches", () => {
    // A long straight leg, one sharp turn, then another long straight leg: the
    // corner is the only place an external router could choose differently.
    const straightThenTurn: GeoPoint[] = [
      ...Array.from({ length: 30 }, (_, index) => ({ latitude: 55.7, longitude: 37.6 + index * 0.004 })),
      ...Array.from({ length: 30 }, (_, index) => ({ latitude: 55.7 + (index + 1) * 0.004, longitude: 37.716 })),
    ];
    const points = externalRoutePoints(straightThenTurn, 4);
    expect(points.length).toBeLessThanOrEqual(4);
    // The point guarding the corner sits just past it, on the outgoing leg: on
    // the apex itself it could snap to the wrong carriageway.
    const guard = points.find((point) => Math.abs(point.longitude - 37.716) < 1e-9 && point.latitude > 55.7);
    expect(guard, "the corner must be guarded from just past the apex").toBeDefined();
    expect(guard!.latitude - 55.7).toBeLessThan(0.005);
  });

  it("passes short geometries through untouched", () => {
    const short = geometry.slice(0, 3);
    expect(externalRoutePoints(short)).toEqual(short);
  });

  it("builds a 2GIS directions link with lon,lat via points", () => {
    const url = twoGisRouteUrl({ geometry: geometry.slice(0, 2) })!;
    expect(url.startsWith("https://2gis.ru/directions/points/")).toBe(true);
    expect(decodeURIComponent(new URL(url).pathname)).toBe("/directions/points/37.6,55.7|37.603,55.702");
  });

  it("gives Yandex a longer via chain than 2GIS, since its URL allows it", () => {
    // A straight line reduces to two points whatever the budget, so the budgets
    // are only distinguishable on a line that actually turns.
    const zigzag: GeoPoint[] = Array.from({ length: 60 }, (_, index) => ({
      latitude: 55.7 + index * 0.004,
      longitude: 37.6 + (index % 2 === 0 ? 0 : 0.01),
    }));
    const twoGis = decodeURIComponent(new URL(twoGisRouteUrl({ geometry: zigzag })!).pathname).split("|").length;
    const yandex = new URL(yandexRouteUrl({ geometry: zigzag })!).searchParams.get("rtext")!.split("~").length;
    expect(twoGis).toBeLessThanOrEqual(10);
    expect(yandex).toBeGreaterThan(twoGis);
    expect(yandex).toBeLessThanOrEqual(24);
  });

  it("builds a Yandex Maps link with lat,lon via points and the car router", () => {
    const url = new URL(yandexRouteUrl({ geometry: geometry.slice(0, 2) })!);
    expect(url.origin + url.pathname).toBe("https://yandex.ru/maps/");
    expect(url.searchParams.get("rtext")).toBe("55.7,37.6~55.702,37.603");
    expect(url.searchParams.get("rtt")).toBe("auto");
  });

  it("produces no link for a route without a usable line", () => {
    expect(twoGisRouteUrl({ geometry: [] })).toBeUndefined();
    expect(yandexRouteUrl({ geometry: [geometry[0]!] })).toBeUndefined();
  });
});
