"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import type { GeoPoint, RouteCandidate } from "@/lib/schemas";

/**
 * The map shown when no provider key is configured.
 *
 * A demo without a map is not a demo: the previous fallback drew routes over an
 * abstract grid, which told a visitor nothing about where the route goes. These
 * are OpenStreetMap raster tiles — no key, no account, attribution below — with
 * the route drawn on top in the same Web Mercator projection the tiles use.
 *
 * It pans and zooms like a map should; what it is not is a vector map with a
 * congestion layer, which is what the provider renderers are for.
 */
const TILE_SIZE = 256;
const TILE_HOST = "https://tile.openstreetmap.org";
const MIN_ZOOM = 3;
const MAX_ZOOM = 16;
const EDGE_PADDING = 48;

function lonToTileX(longitude: number, zoom: number): number {
  return ((longitude + 180) / 360) * 2 ** zoom;
}

function latToTileY(latitude: number, zoom: number): number {
  const radians = (latitude * Math.PI) / 180;
  return ((1 - Math.log(Math.tan(radians) + 1 / Math.cos(radians)) / Math.PI) / 2) * 2 ** zoom;
}

type Viewport = { zoom: number; originX: number; originY: number };
type Adjustment = { zoom: number; dx: number; dy: number };
const ZERO: Adjustment = { zoom: 0, dx: 0, dy: 0 };

function chooseViewport(points: GeoPoint[], width: number, height: number): Viewport {
  const usableWidth = Math.max(1, width - EDGE_PADDING * 2);
  const usableHeight = Math.max(1, height - EDGE_PADDING * 2);
  const west = Math.min(...points.map((point) => point.longitude));
  const east = Math.max(...points.map((point) => point.longitude));
  const north = Math.max(...points.map((point) => point.latitude));
  const south = Math.min(...points.map((point) => point.latitude));

  let zoom = MIN_ZOOM;
  for (let candidate = MAX_ZOOM; candidate >= MIN_ZOOM; candidate--) {
    const spanX = (lonToTileX(east, candidate) - lonToTileX(west, candidate)) * TILE_SIZE;
    const spanY = (latToTileY(south, candidate) - latToTileY(north, candidate)) * TILE_SIZE;
    if (spanX <= usableWidth && spanY <= usableHeight) {
      zoom = candidate;
      break;
    }
  }

  const centreX = (lonToTileX(west, zoom) + lonToTileX(east, zoom)) / 2;
  const centreY = (latToTileY(north, zoom) + latToTileY(south, zoom)) / 2;
  return {
    zoom,
    originX: centreX * TILE_SIZE - width / 2,
    originY: centreY * TILE_SIZE - height / 2,
  };
}

export function TileMap({
  routes,
  selectedRoute,
  segmentColor,
  label,
  hidden,
  zoomLabels,
}: {
  routes: RouteCandidate[];
  selectedRoute?: RouteCandidate | undefined;
  segmentColor: (segment: RouteCandidate["segments"][number]) => string;
  label: string;
  hidden?: boolean | undefined;
  /** Spoken names for the zoom buttons; "+" tells a screen reader nothing. */
  zoomLabels: { in: string; out: string };
}) {
  const container = useRef<HTMLDivElement>(null);
  const [size, setSize] = useState({ width: 0, height: 0 });
  // Panning and zooming are expressed relative to the fitted view, and tagged
  // with the frame they belong to: when a new route refits the map the offset
  // falls away by itself instead of dragging the old view along.
  const [gesture, setGesture] = useState<{ frame: string; adjust: Adjustment }>({ frame: "", adjust: ZERO });
  const [dragging, setDragging] = useState(false);
  const drag = useRef<{ pointerId: number; x: number; y: number } | null>(null);

  useEffect(() => {
    const element = container.current;
    if (!element) return;
    const observer = new ResizeObserver((entries) => {
      const box = entries[0]?.contentRect;
      if (box) setSize({ width: Math.round(box.width), height: Math.round(box.height) });
    });
    observer.observe(element);
    return () => observer.disconnect();
  }, []);

  const points = useMemo(() => {
    const all = routes.flatMap((route) => route.geometry);
    // Moscow, so an empty planner still shows a city rather than the ocean.
    return all.length >= 2 ? all : [{ latitude: 55.68, longitude: 37.48 }, { latitude: 55.88, longitude: 37.75 }];
  }, [routes]);

  const fitted = useMemo(
    () => (size.width && size.height ? chooseViewport(points, size.width, size.height) : undefined),
    [points, size.width, size.height],
  );

  const frame = fitted ? `${fitted.zoom}:${Math.round(fitted.originX)}:${Math.round(fitted.originY)}` : "";
  const adjust = gesture.frame === frame ? gesture.adjust : ZERO;
  const move = (change: (current: Adjustment) => Adjustment) =>
    setGesture((current) => ({ frame, adjust: change(current.frame === frame ? current.adjust : ZERO) }));

  const view = useMemo(() => {
    if (!fitted) return undefined;
    const zoom = Math.min(MAX_ZOOM, Math.max(MIN_ZOOM, fitted.zoom + adjust.zoom));
    // Zooming keeps the centre of the viewport fixed, which is what a wheel over
    // the middle of a map is expected to do.
    const scale = 2 ** (zoom - fitted.zoom);
    const centreX = (fitted.originX + size.width / 2) * scale;
    const centreY = (fitted.originY + size.height / 2) * scale;
    return {
      zoom,
      originX: centreX - size.width / 2 - adjust.dx,
      originY: centreY - size.height / 2 - adjust.dy,
    };
  }, [fitted, adjust, size.width, size.height]);

  const zoomBy = (delta: number) => move((current) => ({ ...current, zoom: current.zoom + delta }));

  const project = (point: GeoPoint): [number, number] => {
    if (!view) return [0, 0];
    return [
      lonToTileX(point.longitude, view.zoom) * TILE_SIZE - view.originX,
      latToTileY(point.latitude, view.zoom) * TILE_SIZE - view.originY,
    ];
  };

  const path = (geometry: GeoPoint[]) =>
    geometry
      .map((point, index) => {
        const [x, y] = project(point);
        return `${index ? "L" : "M"}${x.toFixed(1)},${y.toFixed(1)}`;
      })
      .join(" ");

  const tiles = useMemo(() => {
    if (!view || !size.width || !size.height) return [];
    const count = 2 ** view.zoom;
    const first = { x: Math.floor(view.originX / TILE_SIZE), y: Math.floor(view.originY / TILE_SIZE) };
    const last = {
      x: Math.floor((view.originX + size.width) / TILE_SIZE),
      y: Math.floor((view.originY + size.height) / TILE_SIZE),
    };
    const result: Array<{ key: string; src: string; left: number; top: number }> = [];
    for (let y = first.y; y <= last.y; y++) {
      if (y < 0 || y >= count) continue;
      for (let x = first.x; x <= last.x; x++) {
        const wrapped = ((x % count) + count) % count;
        result.push({
          key: `${view.zoom}/${x}/${y}`,
          src: `${TILE_HOST}/${view.zoom}/${wrapped}/${y}.png`,
          left: x * TILE_SIZE - view.originX,
          top: y * TILE_SIZE - view.originY,
        });
      }
    }
    return result;
  }, [view, size.width, size.height]);

  const start = selectedRoute?.geometry[0];
  const finish = selectedRoute?.geometry.at(-1);

  return (
    <div
      className={`tile-map${dragging ? " is-dragging" : ""}`}
      ref={container}
      role="img"
      aria-label={label}
      aria-hidden={hidden}
      onPointerDown={(event) => {
        // A press on the zoom control is a click, not the start of a pan: the
        // container used to capture the pointer and swallow the button.
        if (event.button !== 0 || (event.target as HTMLElement).closest(".tile-map-zoom")) return;
        drag.current = { pointerId: event.pointerId, x: event.clientX, y: event.clientY };
        setDragging(true);
        event.currentTarget.setPointerCapture(event.pointerId);
      }}
      onPointerMove={(event) => {
        const active = drag.current;
        if (!active || active.pointerId !== event.pointerId) return;
        const dx = event.clientX - active.x;
        const dy = event.clientY - active.y;
        drag.current = { ...active, x: event.clientX, y: event.clientY };
        move((current) => ({ ...current, dx: current.dx + dx, dy: current.dy + dy }));
      }}
      onPointerUp={(event) => {
        if (drag.current?.pointerId !== event.pointerId) return;
        drag.current = null;
        setDragging(false);
      }}
      onPointerCancel={() => { drag.current = null; setDragging(false); }}
      onWheel={(event) => zoomBy(event.deltaY < 0 ? 1 : -1)}
      onDoubleClick={() => zoomBy(1)}
    >
      <div className="tile-map-tiles" aria-hidden="true">
        {tiles.map((tile) => (
          // eslint-disable-next-line @next/next/no-img-element
          <img key={tile.key} src={tile.src} alt="" loading="lazy" decoding="async" style={{ left: tile.left, top: tile.top }} />
        ))}
      </div>
      {view && (
        <svg className="tile-map-overlay" width={size.width} height={size.height} aria-hidden="true" focusable="false">
          {routes
            .filter((route) => route.candidateId !== selectedRoute?.candidateId)
            .map((route) => (
              <path key={route.candidateId} d={path(route.geometry)} fill="none" stroke="var(--map-route-alternative)" strokeWidth="3" strokeLinecap="round" />
            ))}
          {(selectedRoute?.segments ?? []).map((segment) => (
            <path key={`${segment.segmentId}-casing`} d={path(segment.geometry)} fill="none" stroke="var(--map-route-casing)" strokeWidth="7" strokeLinecap="round" />
          ))}
          {(selectedRoute?.segments ?? []).map((segment) => (
            <path key={segment.segmentId} d={path(segment.geometry)} fill="none" stroke={segmentColor(segment)} strokeWidth="4" strokeLinecap="round" />
          ))}
          {start && (
            <g transform={`translate(${project(start).join(" ")})`}>
              <circle r="15" fill="var(--map-pin-start)" />
              <text fill="var(--map-pin-start-ink)" textAnchor="middle" dominantBaseline="central" fontSize="15" fontWeight="700">A</text>
            </g>
          )}
          {finish && (
            <g transform={`translate(${project(finish).join(" ")})`}>
              <circle r="15" fill="var(--accent)" stroke="var(--map-pin-start)" strokeWidth="4" />
              <text fill="#21201f" textAnchor="middle" dominantBaseline="central" fontSize="15" fontWeight="700">B</text>
            </g>
          )}
        </svg>
      )}
      <div className="tile-map-zoom">
        <button type="button" onClick={() => zoomBy(1)} aria-label={zoomLabels.in} title={zoomLabels.in}>+</button>
        <button type="button" onClick={() => zoomBy(-1)} aria-label={zoomLabels.out} title={zoomLabels.out}>&minus;</button>
      </div>
      <a className="tile-map-credit" href="https://www.openstreetmap.org/copyright" target="_blank" rel="noreferrer noopener">
        © OpenStreetMap
      </a>
    </div>
  );
}
