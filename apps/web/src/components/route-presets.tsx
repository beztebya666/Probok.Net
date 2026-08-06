"use client";

import { useState } from "react";
import { useLocale } from "@/lib/i18n";
import type { RouteBookmark } from "@/lib/route-bookmarks";
import { formatDistance, formatDuration, greenTimePercent } from "@/lib/route-insights";
import type { RoutePreset } from "@/lib/route-presets";
import { BookmarkIcon, ClockIcon, XIcon } from "./icons";

// The panel is already a scroll container, so the recent list stops at the
// newest few; anything older is kept by starring it into favourites.
const RECENT_ROWS = 3;

type Tab = "favorites" | "recent" | "bookmarks";

function Star({ filled }: { filled: boolean }) {
  return (
    <svg aria-hidden="true" viewBox="0 0 24 24" fill={filled ? "currentColor" : "none"} stroke="currentColor" strokeWidth="1.7">
      <path d="m12 3 2.7 5.5 6.1.9-4.4 4.3 1 6.1-5.4-2.9-5.4 2.9 1-6.1-4.4-4.3 6.1-.9L12 3Z" />
    </svg>
  );
}

function PresetRow({
  preset,
  disabled,
  onRestore,
  onToggleFavorite,
}: {
  preset: RoutePreset;
  disabled: boolean;
  onRestore: (preset: RoutePreset) => void;
  onToggleFavorite: (id: string) => void;
}) {
  const { t } = useLocale();
  const routeName = `${preset.origin.label} — ${preset.destination.label}`;
  const trafficLabel = preset.trafficProvider === "2gis"
    ? t("trafficProvider_2gis")
    : preset.trafficProvider === "yandex" ? t("trafficProvider_yandex") : t("trafficProvider_off");
  return (
    <article className="route-preset-row" data-testid={`route-preset-${preset.id}`}>
      <button
        className="route-preset-main"
        type="button"
        disabled={disabled}
        onClick={() => onRestore(preset)}
        aria-label={t("restoreRoute", { value: routeName })}
      >
        <span className="route-preset-points">
          <strong title={preset.origin.label}>{preset.origin.label}</strong>
          <span aria-hidden="true">→</span>
          <strong title={preset.destination.label}>{preset.destination.label}</strong>
        </span>
        <small><ClockIcon /> {t(preset.routingMode)} · {trafficLabel} · +{preset.extraDistanceKm} {t("kilometersShort")} · +{preset.extraTimeMinutes} {t("minutesShort")}</small>
      </button>
      <button
        className={`route-preset-favorite ${preset.favorite ? "is-favorite" : ""}`}
        type="button"
        disabled={disabled}
        aria-pressed={preset.favorite}
        aria-label={preset.favorite ? t("removeFavorite", { value: routeName }) : t("addFavorite", { value: routeName })}
        title={preset.favorite ? t("removeFavoriteShort") : t("addFavoriteShort")}
        onClick={() => onToggleFavorite(preset.id)}
      >
        <Star filled={preset.favorite} />
      </button>
    </article>
  );
}

function BookmarkRow({
  bookmark,
  active,
  disabled,
  onOpen,
  onRemove,
}: {
  bookmark: RouteBookmark;
  active: boolean;
  disabled: boolean;
  onOpen: (bookmark: RouteBookmark) => void;
  onRemove: (id: string) => void;
}) {
  const { locale, t } = useLocale();
  const route = bookmark.result.greenTopRoutes[0] ?? bookmark.result.selectedRoute ?? bookmark.result.fastestReferenceRoute;
  return (
    <article className={`route-bookmark-row ${active ? "is-active" : ""}`} data-testid={`route-bookmark-${bookmark.id}`}>
      <button
        className="route-bookmark-main"
        type="button"
        disabled={disabled}
        onClick={() => onOpen(bookmark)}
        aria-label={t("openBookmark", { value: bookmark.label })}
      >
        <span className="route-bookmark-label" title={bookmark.label}>{bookmark.label}</span>
        {route && (
          <small>
            <b>{t("greenShareShort", { value: greenTimePercent(route) })}</b>
            {" · "}{formatDuration(route.liveDurationSeconds, locale)}
            {" · "}{formatDistance(route.distanceMeters, locale)}
          </small>
        )}
        <small className="route-bookmark-stamp">
          {t("measuredAt", { value: new Date(bookmark.result.generatedAt).toLocaleString(locale) })}
        </small>
      </button>
      <button
        className="route-bookmark-remove"
        type="button"
        disabled={disabled}
        aria-label={t("removeBookmark", { value: bookmark.label })}
        title={t("removeBookmarkShort")}
        onClick={() => onRemove(bookmark.id)}
      >
        <XIcon />
      </button>
    </article>
  );
}

/**
 * One block, three views. Favourites, recent searches and saved analyses each
 * answer a different question, but only one of them at a time, which keeps the
 * planner short instead of stacking three lists nobody scrolled to.
 */
export function RoutePresets({
  entries,
  ready,
  disabled,
  onRestore,
  onToggleFavorite,
  bookmarks = [],
  activeSearchId,
  onOpenBookmark,
  onRemoveBookmark,
}: {
  entries: RoutePreset[];
  ready: boolean;
  disabled: boolean;
  onRestore: (preset: RoutePreset) => void;
  onToggleFavorite: (id: string) => void;
  bookmarks?: RouteBookmark[] | undefined;
  activeSearchId?: string | undefined;
  onOpenBookmark?: ((bookmark: RouteBookmark) => void) | undefined;
  onRemoveBookmark?: ((id: string) => void) | undefined;
}) {
  const { t } = useLocale();
  const [tab, setTab] = useState<Tab>("favorites");
  if (!ready) return null;

  const favorites = entries.filter((entry) => entry.favorite);
  const recents = entries.filter((entry) => !entry.favorite).slice(0, RECENT_ROWS);
  const tabs: Array<{ id: Tab; label: string; icon: React.ReactNode; count: number }> = [
    { id: "favorites", label: t("tabFavorites"), icon: <Star filled />, count: favorites.length },
    { id: "recent", label: t("tabRecent"), icon: <ClockIcon />, count: recents.length },
    { id: "bookmarks", label: t("tabBookmarks"), icon: <BookmarkIcon />, count: bookmarks.length },
  ];

  return (
    // The tabs name the section well enough; the heading and the privacy glyph
    // only cost vertical space in a panel that is already tall.
    <section className="route-presets" aria-label={t("savedRoutes")} title={t("localRoutesPrivacyTitle")}>
      <div className="route-presets-tabs" role="tablist" aria-label={t("savedRoutes")}>
        {tabs.map((item) => (
          <button
            key={item.id}
            type="button"
            role="tab"
            aria-selected={tab === item.id}
            aria-label={item.label}
            title={item.label}
            className={tab === item.id ? "is-active" : ""}
            data-testid={`preset-tab-${item.id}`}
            onClick={() => setTab(item.id)}
          >
            {item.icon}
            {item.count > 0 && <i>{item.count}</i>}
          </button>
        ))}
      </div>

      {tab === "favorites" && (
        favorites.length > 0 ? (
          <div className="route-preset-list">
            {favorites.map((preset) => <PresetRow key={preset.id} preset={preset} disabled={disabled} onRestore={onRestore} onToggleFavorite={onToggleFavorite} />)}
          </div>
        ) : <p className="route-presets-empty">{t("favoritesEmpty")}</p>
      )}

      {tab === "recent" && (
        recents.length > 0 ? (
          <div className="route-preset-list">
            {recents.map((preset) => <PresetRow key={preset.id} preset={preset} disabled={disabled} onRestore={onRestore} onToggleFavorite={onToggleFavorite} />)}
          </div>
        ) : <p className="route-presets-empty">{favorites.length > 0 ? t("allRecentFavorited") : t("recentRoutesEmpty")}</p>
      )}

      {tab === "bookmarks" && (
        bookmarks.length > 0 && onOpenBookmark && onRemoveBookmark ? (
          <div className="route-preset-list">
            {bookmarks.map((bookmark) => (
              <BookmarkRow
                key={bookmark.id}
                bookmark={bookmark}
                active={bookmark.result.searchId === activeSearchId}
                disabled={disabled}
                onOpen={onOpenBookmark}
                onRemove={onRemoveBookmark}
              />
            ))}
          </div>
        ) : <p className="route-presets-empty">{t("bookmarksEmpty")}</p>
      )}
    </section>
  );
}
