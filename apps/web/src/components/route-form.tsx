"use client";

import { useEffect, useRef, useState } from "react";
import { useLocale } from "@/lib/i18n";
import {
  parseRoutePresets,
  readRoutePresets,
  rememberRoutePreset,
  ROUTE_PRESETS_STORAGE_KEY,
  toggleRoutePresetFavorite,
  writeRoutePresets,
  type RoutePreset,
  type RoutePresetDraft,
  type TrafficProvider,
} from "@/lib/route-presets";
import {
  defaultRoutePreferences,
  extraDistancePercentFor,
  MAX_EXTRA_DISTANCE_KM,
  MAX_EXTRA_TIME_MINUTES,
  parseRoutePreferences,
  providerRequestBudgetFor,
  strictnessFor,
  readRoutePreferences,
  ROUTE_PREFERENCES_STORAGE_KEY,
  writeRoutePreferences,
} from "@/lib/route-preferences";
import type { RouteBookmark } from "@/lib/route-bookmarks";
import type { RouteSearchRequest, RoutingMode } from "@/lib/schemas";
import { ArrowIcon, PlusIcon, SwapIcon, XIcon } from "./icons";
import { LocationField, type LocationFieldHandle, type LocationValue } from "./location-field";
import { RoutePresets } from "./route-presets";

type Props = {
  onSubmit: (request: RouteSearchRequest) => void | Promise<void>;
  busy: boolean;
  configured: boolean;
  trafficProvider: TrafficProvider;
  onTrafficProviderChange: (provider: TrafficProvider) => void;
  // Search status belongs directly under the button that started it, not below
  // the saved-routes list at the bottom of the panel.
  status?: React.ReactNode;
  // The resolved "A → B" text, reported on submit so a bookmark of the result
  // can be labelled with the addresses the user actually typed.
  onRouteLabelChange?: (label: string) => void;
  // Saved analyses live in the same block as favourites and recent searches.
  bookmarks?: RouteBookmark[] | undefined;
  activeSearchId?: string | undefined;
  onOpenBookmark?: ((bookmark: RouteBookmark) => void) | undefined;
  onRemoveBookmark?: ((id: string) => void) | undefined;
  // The demo build opens with a finished analysis; the planner should show the
  // trip it belongs to instead of two blank fields.
  initialRoute?: { origin: LocationValue; destination: LocationValue } | undefined;
};

function safeLocalStorage(): Storage | undefined {
  try {
    return window.localStorage;
  } catch {
    // Private browsing and blocked storage make localStorage throw on access.
    return undefined;
  }
}

function uuid(): string {
  if (typeof crypto.randomUUID === "function") return crypto.randomUUID();
  const bytes = crypto.getRandomValues(new Uint8Array(16));
  bytes[6] = (bytes[6]! & 0x0f) | 0x40;
  bytes[8] = (bytes[8]! & 0x3f) | 0x80;
  const value = [...bytes].map((byte) => byte.toString(16).padStart(2, "0")).join("");
  return `${value.slice(0, 8)}-${value.slice(8, 12)}-${value.slice(12, 16)}-${value.slice(16, 20)}-${value.slice(20)}`;
}

export function RouteForm({ onSubmit, busy, configured, trafficProvider, onTrafficProviderChange, status, onRouteLabelChange, bookmarks, activeSearchId, onOpenBookmark, onRemoveBookmark, initialRoute }: Props) {
  const { t } = useLocale();
  const [origin, setOrigin] = useState<LocationValue>(initialRoute?.origin ?? { label: "" });
  const [destination, setDestination] = useState<LocationValue>(initialRoute?.destination ?? { label: "" });
  const [waypoints, setWaypoints] = useState<LocationValue[]>([]);
  const [mode, setMode] = useState<RoutingMode>(defaultRoutePreferences.routingMode);
  const [extraDistanceKm, setExtraDistanceKm] = useState(defaultRoutePreferences.extraDistanceKm);
  const [extraTimeMinutes, setExtraTimeMinutes] = useState(defaultRoutePreferences.extraTimeMinutes);
  const [avoidTolls, setAvoidTolls] = useState(defaultRoutePreferences.avoidTolls);
  const [avoidUnpaved, setAvoidUnpaved] = useState(defaultRoutePreferences.avoidUnpaved);
  const [preferencesReady, setPreferencesReady] = useState(false);
  const [submitted, setSubmitted] = useState(false);
  const [resolving, setResolving] = useState(false);
  const [routePresets, setRoutePresets] = useState<RoutePreset[]>([]);
  const [routePresetsReady, setRoutePresetsReady] = useState(false);
  const submitInProgress = useRef(false);
  const originField = useRef<LocationFieldHandle>(null);
  const destinationField = useRef<LocationFieldHandle>(null);
  const waypointFields = useRef<Array<LocationFieldHandle | null>>([]);

  const invalidOrigin = submitted && !origin.point;
  const invalidDestination = submitted && !destination.point;
  const invalidWaypoint = submitted && waypoints.some((waypoint) => !waypoint.point);

  useEffect(() => {
    let active = true;
    let storage: Storage | undefined;
    try {
      storage = window.localStorage;
    } catch {
      storage = undefined;
    }
    const initial = readRoutePresets(storage);
    window.queueMicrotask(() => {
      if (!active) return;
      setRoutePresets(initial);
      setRoutePresetsReady(true);
    });
    const syncStorage = (event: StorageEvent) => {
      if (event.key === ROUTE_PRESETS_STORAGE_KEY) setRoutePresets(parseRoutePresets(event.newValue));
    };
    window.addEventListener("storage", syncStorage);
    return () => {
      active = false;
      window.removeEventListener("storage", syncStorage);
    };
  }, []);

  useEffect(() => {
    let active = true;
    const applyPreferences = (preferences: ReturnType<typeof readRoutePreferences>) => {
      setMode(preferences.routingMode);
      setExtraDistanceKm(preferences.extraDistanceKm);
      setExtraTimeMinutes(preferences.extraTimeMinutes);
      setAvoidTolls(preferences.avoidTolls);
      setAvoidUnpaved(preferences.avoidUnpaved);
    };
    const stored = readRoutePreferences(safeLocalStorage());
    window.queueMicrotask(() => {
      if (!active) return;
      applyPreferences(stored);
      setPreferencesReady(true);
    });
    const syncStorage = (event: StorageEvent) => {
      if (event.key === ROUTE_PREFERENCES_STORAGE_KEY) applyPreferences(parseRoutePreferences(event.newValue));
    };
    window.addEventListener("storage", syncStorage);
    return () => {
      active = false;
      window.removeEventListener("storage", syncStorage);
    };
  }, []);

  // Persist only after the stored preferences were applied, so the very first
  // render never overwrites them with component defaults.
  useEffect(() => {
    if (!preferencesReady) return;
    writeRoutePreferences(
      { routingMode: mode, extraDistanceKm, extraTimeMinutes, avoidTolls, avoidUnpaved },
      safeLocalStorage(),
    );
  }, [preferencesReady, mode, extraDistanceKm, extraTimeMinutes, avoidTolls, avoidUnpaved]);

  const persistPresets = (update: (current: RoutePreset[]) => RoutePreset[]) => {
    setRoutePresets((current) => {
      const next = update(current);
      let storage: Storage | undefined;
      try {
        storage = window.localStorage;
      } catch {
        storage = undefined;
      }
      writeRoutePresets(next, storage);
      return next;
    });
  };

  const restorePreset = (preset: RoutePreset) => {
    setOrigin(preset.origin);
    setDestination(preset.destination);
    setWaypoints(preset.waypoints);
    setMode(preset.routingMode);
    setExtraDistanceKm(preset.extraDistanceKm);
    setExtraTimeMinutes(preset.extraTimeMinutes);
    setAvoidTolls(preset.avoidTolls);
    setAvoidUnpaved(preset.avoidUnpaved);
    onTrafficProviderChange(preset.trafficProvider);
    setSubmitted(false);
    const draft: RoutePresetDraft = {
      origin: preset.origin,
      destination: preset.destination,
      waypoints: preset.waypoints,
      routingMode: preset.routingMode,
      extraDistanceKm: preset.extraDistanceKm,
      extraTimeMinutes: preset.extraTimeMinutes,
      avoidTolls: preset.avoidTolls,
      avoidUnpaved: preset.avoidUnpaved,
      trafficProvider: preset.trafficProvider,
    };
    persistPresets((current) => rememberRoutePreset(current, draft, preset.id, Date.now()));
  };

  const submit = async (event: React.FormEvent) => {
    event.preventDefault();
    setSubmitted(true);
    if (!configured || busy || submitInProgress.current) return;

    submitInProgress.current = true;
    setResolving(true);
    try {
      const [resolvedOrigin, resolvedDestination, resolvedWaypoints] = await Promise.all([
        origin.point ? Promise.resolve(origin) : originField.current?.resolveFirstSuggestion(),
        destination.point ? Promise.resolve(destination) : destinationField.current?.resolveFirstSuggestion(),
        Promise.all(waypoints.map((waypoint, index) =>
          waypoint.point ? Promise.resolve(waypoint) : waypointFields.current[index]?.resolveFirstSuggestion(),
        )),
      ]);

      if (!resolvedOrigin?.point || !resolvedDestination?.point || resolvedWaypoints.some((waypoint) => !waypoint?.point)) return;

      onRouteLabelChange?.(`${resolvedOrigin.label} → ${resolvedDestination.label}`);
      await onSubmit({
        requestId: uuid(),
        origin: resolvedOrigin.point,
        destination: resolvedDestination.point,
        waypoints: resolvedWaypoints.map((waypoint) => waypoint!.point!),
        departureTime: new Date().toISOString(),
        routingMode: mode,
        maxExtraDistanceMeters: extraDistanceKm * 1_000,
        maxExtraDistancePercent: extraDistancePercentFor(mode),
        maxExtraTimeSeconds: extraTimeMinutes * 60,
        avoidTolls,
        avoidUnpaved,
        strictness: strictnessFor(mode),
        maxProviderRequests: providerRequestBudgetFor(mode),
        searchDeadlineMs: 20_000,
      });
      const preset: RoutePresetDraft = {
        origin: { label: resolvedOrigin.label, point: resolvedOrigin.point },
        destination: { label: resolvedDestination.label, point: resolvedDestination.point },
        waypoints: resolvedWaypoints.map((waypoint) => ({ label: waypoint!.label, point: waypoint!.point! })),
        routingMode: mode,
        extraDistanceKm,
        extraTimeMinutes,
        avoidTolls,
        avoidUnpaved,
        trafficProvider,
      };
      persistPresets((current) => rememberRoutePreset(current, preset, uuid(), Date.now()));
    } finally {
      submitInProgress.current = false;
      setResolving(false);
    }
  };

  const formDisabled = busy || resolving;

  const modes: RoutingMode[] = ["FASTEST", "BALANCED", "GREENEST", "STRICT_GREEN"];

  return (
    <form className="route-form" onSubmit={submit} noValidate>
      <h1 className="sr-only">{t("plannerTitle")}</h1>

      <div className="location-stack">
        <LocationField
          ref={originField}
          label={t("origin")}
          value={origin}
          onChange={setOrigin}
          allowGeolocation
          required
          disabled={formDisabled}
          error={invalidOrigin ? t("selectSuggestion") : undefined}
        />
        {waypoints.map((waypoint, index) => (
          <div className="waypoint-row" key={`waypoint-${index}`}>
            <LocationField
              ref={(handle) => { waypointFields.current[index] = handle; }}
              label={`${t("waypoint")} ${index + 1}`}
              value={waypoint}
              onChange={(next) => setWaypoints((current) => current.map((item, itemIndex) => itemIndex === index ? next : item))}
              disabled={formDisabled}
              error={invalidWaypoint && !waypoint.point ? t("selectSuggestion") : undefined}
            />
            <button
              type="button"
              className="icon-button remove-waypoint"
              aria-label={t("removeWaypoint")}
              disabled={formDisabled}
              onClick={() => setWaypoints((current) => current.filter((_, itemIndex) => itemIndex !== index))}
            >
              <XIcon />
            </button>
          </div>
        ))}
        <LocationField
          ref={destinationField}
          label={t("destination")}
          value={destination}
          onChange={setDestination}
          required
          disabled={formDisabled}
          action={
            <button
              type="button"
              className="icon-button swap-button"
              aria-label={`${t("origin")} / ${t("destination")}`}
              disabled={formDisabled}
              // A pointer press must not leave the control focused: the swap is
              // done, and a lingering focus ring reads as "this is now active".
              // Keyboard focus is untouched, so Tab still shows the ring.
              onMouseDown={(event) => event.preventDefault()}
              onClick={() => { setOrigin(destination); setDestination(origin); }}
            >
              <SwapIcon />
            </button>
          }
          error={invalidDestination ? t("selectSuggestion") : undefined}
        />
      </div>

      {waypoints.length < 3 && (
        <button className="text-button" type="button" disabled={formDisabled} onClick={() => setWaypoints((current) => [...current, { label: "" }])}>
          <PlusIcon /> {t("addWaypoint")}
        </button>
      )}

      <fieldset className="mode-fieldset" disabled={formDisabled}>
        <legend>{t("routeMode")}</legend>
        <div className="mode-grid">
          {modes.map((item) => (
            <label className={`mode-card ${mode === item ? "is-active" : ""}`} key={item}>
              <input type="radio" name="routing-mode" value={item} checked={mode === item} onChange={() => setMode(item)} />
              <span className="mode-title">{t(item)}</span>
              <span className="mode-description">{t(`${item}_desc` as `${RoutingMode}_desc`)}</span>
            </label>
          ))}
        </div>
      </fieldset>

      <details className="advanced-settings">
        <summary>
          <span>{t("limits")}</span>
          <span className="advanced-summary-values">+{extraDistanceKm} {t("kilometersShort")} · +{extraTimeMinutes} {t("minutesShort")}</span>
        </summary>
        <fieldset className="limits-fieldset" disabled={formDisabled}>
          <legend className="sr-only">{t("limits")}</legend>
          <label className="range-field">
            <span>{t("extraDistance", { value: extraDistanceKm })}</span>
            <input type="range" min="0" max={MAX_EXTRA_DISTANCE_KM} step="5" value={extraDistanceKm} onChange={(event) => setExtraDistanceKm(Number(event.target.value))} />
          </label>
          <label className="range-field">
            <span>{t("extraTime", { value: extraTimeMinutes })}</span>
            <input type="range" min="0" max={MAX_EXTRA_TIME_MINUTES} step="5" value={extraTimeMinutes} onChange={(event) => setExtraTimeMinutes(Number(event.target.value))} />
          </label>
          <div className="toggle-row">
            <label className="check-field"><input type="checkbox" checked={avoidTolls} onChange={(event) => setAvoidTolls(event.target.checked)} /><span>{t("avoidTolls")}</span></label>
            <label className="check-field"><input type="checkbox" checked={avoidUnpaved} onChange={(event) => setAvoidUnpaved(event.target.checked)} /><span>{t("avoidUnpaved")}</span></label>
          </div>
        </fieldset>
      </details>

      <button className="button button-primary search-button" type="submit" disabled={formDisabled || !configured}>
        <span>{formDisabled ? t("searchInProgress") : t("buildRoute")}</span><ArrowIcon />
      </button>

      {status}

      <RoutePresets
        entries={routePresets}
        ready={routePresetsReady}
        disabled={formDisabled}
        onRestore={restorePreset}
        onToggleFavorite={(id) => persistPresets((current) => toggleRoutePresetFavorite(current, id))}
        bookmarks={bookmarks}
        activeSearchId={activeSearchId}
        onOpenBookmark={onOpenBookmark}
        onRemoveBookmark={onRemoveBookmark}
      />
    </form>
  );
}
