"use client";

import { useMemo } from "react";
import { useLocale, warningLabel } from "@/lib/i18n";
import {
	congestionSeconds,
  estimatedFreePercent,
  findGreenest,
  formatDistance,
  formatDuration,
  displayedRoutes,
  greenTimePercent,
  isVerifiedAllGreenRoute,
} from "@/lib/route-insights";
import { twoGisRouteUrl, yandexRouteUrl } from "@/lib/external-links";
import type { RouteCandidate, RouteSearchResult, RoutingMode } from "@/lib/schemas";
import { AlertIcon, BookmarkIcon, CheckIcon, ClockIcon, InfoIcon, RefreshIcon, XIcon } from "./icons";

/**
 * Opens the found route in a consumer navigator. The link carries via points
 * sampled from the geometry, so the external map follows this route instead of
 * re-planning its own between the same two addresses.
 */
function ExternalRouteLinks({ route, compact }: { route: RouteCandidate; compact?: boolean }) {
  const { t } = useLocale();
  const twoGis = twoGisRouteUrl(route);
  const yandex = yandexRouteUrl(route);
  if (!twoGis && !yandex) return null;

  return (
    <div className={`external-links ${compact ? "is-compact" : ""}`}>
      <span className="external-links-label">{t("openRouteIn")}</span>
      {twoGis && (
        <a className="external-link" href={twoGis} target="_blank" rel="noreferrer noopener" onClick={(event) => event.stopPropagation()}>
          {t("open2gis")}
        </a>
      )}
      {yandex && (
        <a className="external-link" href={yandex} target="_blank" rel="noreferrer noopener" onClick={(event) => event.stopPropagation()}>
          {t("openYandex")}
        </a>
      )}
    </div>
  );
}

function RouteCard({
  route,
  selected,
  recommended,
  fastest,
  greenest,
  rank,
  unqualified,
  onSelect,
}: {
  route: RouteCandidate;
  selected: boolean;
  recommended: boolean;
  fastest: boolean;
  greenest: boolean;
  // Position in the green ranking, which is what the list is sorted by.
  rank?: number | undefined;
  unqualified?: boolean | undefined;
  onSelect: () => void;
}) {
  const { locale, t } = useLocale();

  return (
    <article className={`route-card ${selected ? "is-selected" : ""}`} data-testid={`route-card-${route.candidateId}`}>
      <div className="route-card-top">
        <div className="route-badges">
          {rank !== undefined && !fastest && <span className="badge badge-rank">#{rank}</span>}
          {recommended && <span className="badge badge-recommended">{t("recommended")}</span>}
          {fastest && <span className="badge">{t("fastest")}</span>}
          {greenest && !recommended && <span className="badge badge-green">{t("greenest")}</span>}
          {/* A toll is a decision, not a footnote — and one glyph carries it. */}
          {route.tolls && <span className="badge badge-toll" title={t("tolls")} aria-label={t("tolls")}>₽</span>}
          {unqualified && <span className="badge badge-unqualified" title={t("notStrictGreenHint")}>{t("notStrictGreen")}</span>}
        </div>
      </div>

      <div className="route-primary-metrics">
        <div><ClockIcon /><span><strong>{formatDuration(route.liveDurationSeconds, locale)}</strong><small>{t("eta")}</small></span></div>
        <div><span className="metric-symbol" aria-hidden="true">↔</span><span><strong>{formatDistance(route.distanceMeters, locale)}</strong><small>{t("distance")}</small></span></div>
        <div><span className="metric-symbol" aria-hidden="true">+</span><span><strong>{formatDuration(route.trafficDelaySeconds, locale)}</strong><small>{t("delay")}</small></span></div>
        {/* The ranking criterion belongs with the other numbers, not in a banner
            above them. */}
        <div><span className="metric-dot flow-green" aria-hidden="true" /><span><strong>{greenTimePercent(route)}%</strong><small>{t("greenShort")}</small></span></div>
      </div>
	  <div className="route-flow-metrics" aria-label={t("congestionBreakdown")}>
		<span><i className="flow-red" />{t("redDuration")}: <strong>{formatDuration(congestionSeconds(route, "RED"), locale)}</strong></span>
		<span><i className="flow-orange" />{t("orangeDuration")}: <strong>{formatDuration(congestionSeconds(route, "ORANGE"), locale)}</strong></span>
	  </div>

      <div className="flow-summary">
        <div className="flow-bar" aria-label={t("estimatedFree", { value: estimatedFreePercent(route) })}>
          {route.segments.map((segment) => (
            <span
              key={segment.segmentId}
              className={`flow-${segment.congestionClass.toLowerCase()}`}
              style={{ flexGrow: segment.distanceMeters }}
              title={segment.congestionClass}
            />
          ))}
        </div>
      </div>

      {route.unpaved && (
        <div className="route-cautions"><span><AlertIcon />{t("unpaved")}</span></div>
      )}

      {/* Marked, not withheld: the badge says the route is not green throughout,
          and choosing it stays the reader's call. */}
      <button className={`button ${selected ? "button-selected" : "button-secondary"}`} type="button" onClick={onSelect} disabled={selected}>
        {selected ? <><CheckIcon /> {t("selected")}</> : t("choose")}
      </button>
      <ExternalRouteLinks route={route} />
    </article>
  );
}

const USER_FACING_WARNINGS = new Set([
  "PROVIDER_DEGRADED",
  "PROVIDER_QUOTA_EXHAUSTED",
  "PROVIDER_RATE_LIMITED",
  "GREEN_OPTIMIZATION_UNAVAILABLE",
  "DETOUR_SEARCH_STOPPED_EARLY",
  "PARTIAL_RESULTS",
]);

export function RouteResults({
  result,
  routingMode,
  selectedCandidateId,
  onSelect,
  bookmarked,
  onSaveBookmark,
  onRefresh,
  refreshing,
  onDismiss,
}: {
  result: RouteSearchResult;
  routingMode?: RoutingMode | undefined;
  selectedCandidateId?: string | undefined;
  onSelect: (candidateId: string) => void;
  bookmarked?: boolean | undefined;
  onSaveBookmark?: (() => void) | undefined;
  // Re-runs the same search so the measured-at moment becomes current.
  onRefresh?: (() => void) | undefined;
  refreshing?: boolean | undefined;
  // Clears the analysis from the screen and from local storage, so a reload
  // starts from a clean planner instead of restoring what was dismissed.
  onDismiss?: (() => void) | undefined;
}) {
  const { locale, t } = useLocale();
  // The same set the map draws.
  const listed = useMemo(() => displayedRoutes(result, routingMode), [result, routingMode]);
  const measuredAt = useMemo(() => {
    const stamp = Date.parse(result.generatedAt);
    return Number.isFinite(stamp) ? new Date(stamp).toLocaleString(locale) : undefined;
  }, [result.generatedAt, locale]);
  const strictGreenUnavailable = routingMode === "STRICT_GREEN" && !isVerifiedAllGreenRoute(result.selectedRoute);
  const greenest = findGreenest(listed);
  const reference = result.fastestReferenceRoute ?? [...listed].sort((a, b) => a.liveDurationSeconds - b.liveDurationSeconds)[0];
  const recommendedId = result.selectedRoute?.candidateId;
  // Green share is the ranking this product is about, so it is the order of the
  // list; the reference route trails it as the thing being compared against.
  const ordered = [...listed].sort((a, b) => {
    if (a.candidateId === reference?.candidateId) return 1;
    if (b.candidateId === reference?.candidateId) return -1;
    // Green share decides the order, full stop. A toll is shown on the card so
    // the choice is informed, but it does not reorder anything: the product
    // ranks by how much of the drive is free-flowing, not by what it costs.
    return greenTimePercent(b) - greenTimePercent(a) || a.liveDurationSeconds - b.liveDurationSeconds;
  });
  // Most warnings are engineering telemetry: billing estimates, provider
  // bookkeeping, codes with no translation. The panel keeps the few that change
  // what the reader should believe about the answer; the rest stay in the API.
  const actionable = result.warnings.filter((warning) => USER_FACING_WARNINGS.has(warning.code));
  const degraded = result.status === "DEGRADED" || result.warnings.some((warning) => ["PROVIDER_DEGRADED", "GREEN_OPTIMIZATION_UNAVAILABLE"].includes(warning.code));

  return (
    <section className="results-content" aria-labelledby="route-results-title">
      <div className="results-heading">
        <h2 id="route-results-title">{t("resultTitle")}</h2>
        <div className="results-heading-actions">
          {onRefresh && (
            <button
              className="results-refresh"
              type="button"
              onClick={onRefresh}
              disabled={refreshing}
              aria-label={t("refreshResult")}
              title={t("refreshResult")}
              data-testid="refresh-result"
            >
              <RefreshIcon />
            </button>
          )}
          {onSaveBookmark && ordered.length > 0 && (
            <button
              className={`results-bookmark ${bookmarked ? "is-saved" : ""}`}
              type="button"
              onClick={onSaveBookmark}
              aria-pressed={Boolean(bookmarked)}
              aria-label={bookmarked ? t("unsaveBookmark") : t("saveBookmark")}
              title={bookmarked ? t("unsaveBookmark") : t("saveBookmark")}
              data-testid="save-bookmark"
            >
              {/* Filled when saved: an outline alone did not read as "kept". */}
              <BookmarkIcon fill={bookmarked ? "currentColor" : "none"} />
            </button>
          )}
          {onDismiss && (
            <button
              className="icon-button results-dismiss"
              type="button"
              onClick={onDismiss}
              aria-label={t("dismissResult")}
              title={t("dismissResult")}
              data-testid="dismiss-result"
            >
              <XIcon />
            </button>
          )}
        </div>
      </div>


      {!strictGreenUnavailable && degraded && (
        <div className="notice notice-warning" role="status"><AlertIcon /><div><strong>{t("degradedTitle")}</strong><p>{t("degradedBody")}</p></div></div>
      )}
      {!strictGreenUnavailable && actionable.length > 0 && (
        <div className="warning-list" aria-label={t("warningTitle")}>
          {actionable.map((warning, index) => (
            <p key={`${warning.code}-${index}`}><InfoIcon />{warningLabel(warning.code, warning.message, locale)}</p>
          ))}
        </div>
      )}

      <div className="route-list">
        {ordered.map((route, index) => (
          <RouteCard
            key={route.candidateId}
            route={route}
            selected={route.candidateId === selectedCandidateId}
            recommended={route.candidateId === recommendedId}
            fastest={route.candidateId === reference?.candidateId}
            greenest={route.candidateId === greenest?.candidateId}
            rank={index + 1}
            // In strict mode a route that is not verified green throughout is
            // evidence of the search, not an answer it may offer.
            unqualified={routingMode === "STRICT_GREEN" && !isVerifiedAllGreenRoute(route)}
            onSelect={() => onSelect(route.candidateId)}
          />
        ))}
      </div>

      {measuredAt && ordered.length > 0 && (
        <p className="green-podium-hint">
          <strong>{t("measuredAt", { value: measuredAt })} </strong>
          {t("greenTopHint")}
        </p>
      )}
    </section>
  );
}
