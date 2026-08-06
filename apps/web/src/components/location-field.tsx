"use client";

import { useQuery, useQueryClient } from "@tanstack/react-query";
import { forwardRef, useCallback, useEffect, useId, useImperativeHandle, useRef, useState } from "react";
import { geosuggest } from "@/lib/api-client";
import { useLocale } from "@/lib/i18n";
import type { GeoPoint, GeoSuggestion } from "@/lib/schemas";
import { CrosshairIcon, PinIcon } from "./icons";

export type LocationValue = { label: string; point?: GeoPoint };

export type LocationFieldHandle = {
  resolveFirstSuggestion: () => Promise<LocationValue | undefined>;
};

const suggestionStaleTime = 60_000;
const suggestionDebounceMs = 480;
const automaticSuggestionMinLength = 3;
const explicitSuggestionMinLength = 2;

function useDebounced<T>(value: T, delay: number): T {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    const timer = window.setTimeout(() => setDebounced(value), delay);
    return () => window.clearTimeout(timer);
  }, [delay, value]);
  return debounced;
}

type Props = {
  label: string;
  value: LocationValue;
  onChange: (value: LocationValue) => void;
  required?: boolean;
  allowGeolocation?: boolean;
  disabled?: boolean;
  error?: string | undefined;
  // An extra control rendered inside the field, where the geolocation button
  // sits. Used for swap, so it does not need a row of its own.
  action?: React.ReactNode;
};

export const LocationField = forwardRef<LocationFieldHandle, Props>(function LocationField(
  { label, value, onChange, required, allowGeolocation, disabled, error, action },
  ref,
) {
  const { locale, t } = useLocale();
  const queryClient = useQueryClient();
  const inputId = useId();
  const listboxId = `${inputId}-listbox`;
  const currentQuery = value.label.trim();
  const debounced = useDebounced(currentQuery, suggestionDebounceMs);
  const [open, setOpen] = useState(false);
  const [activeIndex, setActiveIndex] = useState(-1);
  const [geoError, setGeoError] = useState(false);
  const blurTimer = useRef<number | undefined>(undefined);
  const latestValue = useRef(value);
  useEffect(() => { latestValue.current = value; }, [value]);

  const suggestionQuery = useCallback((query: string) => ({
    queryKey: ["geosuggest", query, locale] as const,
    queryFn: ({ signal }: { signal: AbortSignal }) => geosuggest(query, locale, signal),
    staleTime: suggestionStaleTime,
  }), [locale]);

  const suggestions = useQuery({
    ...suggestionQuery(debounced),
    enabled: open && debounced.length >= automaticSuggestionMinLength && !value.point,
  });

  // Do not expose a response from the previous debounce cycle as a result for new text.
  const items = debounced === currentQuery ? suggestions.data ?? [] : [];

  const select = useCallback((suggestion: GeoSuggestion): LocationValue => {
    const next = { label: suggestion.label, point: suggestion.point };
    onChange(next);
    setOpen(false);
    setActiveIndex(-1);
    return next;
  }, [onChange]);

  const resolveFirstSuggestion = useCallback(async (): Promise<LocationValue | undefined> => {
    const before = latestValue.current;
    if (before.point) return before;

    const query = before.label.trim();
    if (query.length < explicitSuggestionMinLength) return undefined;

    try {
      const resolved = await queryClient.fetchQuery(suggestionQuery(query));
      const after = latestValue.current;

      // A slow request must never replace input edited while it was in flight.
      if (after.point) return after;
      if (after.label.trim() !== query) return undefined;

      const first = resolved[0];
      return first ? select(first) : undefined;
    } catch {
      setOpen(true);
      return undefined;
    }
  }, [queryClient, select, suggestionQuery]);

  useImperativeHandle(ref, () => ({ resolveFirstSuggestion }), [resolveFirstSuggestion]);

  const useCurrentLocation = () => {
    setGeoError(false);
    if (!("geolocation" in navigator)) {
      setGeoError(true);
      return;
    }
    navigator.geolocation.getCurrentPosition(
      (position) => {
        const point = { latitude: position.coords.latitude, longitude: position.coords.longitude };
        const coordinates = `${point.latitude.toFixed(5)}, ${point.longitude.toFixed(5)}`;
        onChange({ label: `${t("currentLocation")} · ${coordinates}`, point });
        setOpen(false);
        // Coordinates are exact but unreadable. Ask the geocoder what is there
        // and show that instead, while keeping the precise GPS point for the
        // route. If nothing resolves, the coordinates stay — they are correct.
        void queryClient
          .fetchQuery(suggestionQuery(`${point.longitude.toFixed(6)},${point.latitude.toFixed(6)}`))
          .then((nearest) => {
            const first = nearest[0];
            const current = latestValue.current;
            if (!first || current.point?.latitude !== point.latitude || current.point?.longitude !== point.longitude) return;
            onChange({ label: first.label, point });
          })
          .catch(() => undefined);
      },
      () => setGeoError(true),
      { enableHighAccuracy: false, timeout: 8_000, maximumAge: 60_000 },
    );
  };

  const handleKeyDown = (event: React.KeyboardEvent<HTMLInputElement>) => {
    if (event.nativeEvent.isComposing) return;

    if (event.key === "Escape" && open) {
      event.preventDefault();
      setOpen(false);
      return;
    }
    if (!open) return;
    if (event.key === "ArrowDown" && items.length) {
      event.preventDefault();
      setActiveIndex((index) => Math.min(items.length - 1, index + 1));
    } else if (event.key === "ArrowUp" && items.length) {
      event.preventDefault();
      setActiveIndex((index) => Math.max(0, index - 1));
    } else if (event.key === "Enter" && !value.point && currentQuery.length >= explicitSuggestionMinLength) {
      event.preventDefault();
      const selected = items[activeIndex >= 0 ? activeIndex : 0];
      if (selected) select(selected);
      else void resolveFirstSuggestion();
    }
  };

  return (
    <div className={`location-field${allowGeolocation || action ? " has-action" : ""}`}>
      <label className="sr-only" htmlFor={inputId}>{label}</label>
      <span className="location-field-icon" aria-hidden="true"><PinIcon /></span>
      <span className="location-floating-label">{label}</span>
      <input
        id={inputId}
        className="location-input"
        value={value.label}
        disabled={disabled}
        required={required}
        autoComplete="off"
        spellCheck="false"
        placeholder={t("enterAddress")}
        role="combobox"
        aria-autocomplete="list"
        aria-expanded={open && debounced.length >= automaticSuggestionMinLength}
        aria-controls={listboxId}
        aria-activedescendant={activeIndex >= 0 ? `${listboxId}-${activeIndex}` : undefined}
        aria-invalid={Boolean(error)}
        aria-describedby={error ? `${inputId}-error` : undefined}
        onChange={(event) => {
          onChange({ label: event.target.value });
          setOpen(true);
          setActiveIndex(-1);
        }}
        onFocus={() => {
          if (blurTimer.current) window.clearTimeout(blurTimer.current);
          setOpen(true);
        }}
        onBlur={() => { blurTimer.current = window.setTimeout(() => setOpen(false), 140); }}
        onKeyDown={handleKeyDown}
      />
      {action && <span className="location-action">{action}</span>}
      {allowGeolocation && (
        <button
          className="icon-button location-action"
          type="button"
          onClick={useCurrentLocation}
          disabled={disabled}
          aria-label={t("currentLocation")}
          title={t("currentLocation")}
        >
          <CrosshairIcon />
        </button>
      )}

      {open && debounced.length >= automaticSuggestionMinLength && !value.point && (
        <div className="suggestions" id={listboxId} role="listbox" aria-label={label}>
          {suggestions.isFetching && (
            <div className="suggestion-skeletons" role="status" aria-label={t("searchingAddresses")}>
              {[0, 1, 2].map((row) => (
                <span className="suggestion-skeleton" key={row} aria-hidden="true">
                  <span className="suggestion-skeleton-pin" />
                  <span className="suggestion-skeleton-lines">
                    <span /><span />
                  </span>
                </span>
              ))}
            </div>
          )}
          {suggestions.isError && <div className="suggestion-state suggestion-error">{t("noSuggestions")}</div>}
          {!suggestions.isFetching && !suggestions.isError && items.length === 0 && (
            <div className="suggestion-state">{t("noSuggestions")}</div>
          )}
          {items.map((suggestion, index) => (
            <button
              key={suggestion.id}
              id={`${listboxId}-${index}`}
              type="button"
              role="option"
              aria-selected={activeIndex === index}
              className="suggestion"
              onMouseDown={(event) => event.preventDefault()}
              onMouseEnter={() => setActiveIndex(index)}
              onClick={() => select(suggestion)}
            >
              <span className="suggestion-pin" aria-hidden="true"><PinIcon /></span>
              <span><strong>{suggestion.label}</strong>{suggestion.subtitle && <small>{suggestion.subtitle}</small>}</span>
            </button>
          ))}
        </div>
      )}
      {(error || geoError) && <p className="field-error" id={`${inputId}-error`}>{error ?? t("geolocationUnavailable")}</p>}
    </div>
  );
});
