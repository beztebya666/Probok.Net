"use client";

import { useMemo } from "react";
import { reasonLabel, useLocale, warningLabel } from "@/lib/i18n";
import {
	congestionSeconds,
  estimatedFreePercent,
  findGreenest,
  formatDistance,
  formatDuration,
  greenDistancePercent,
  greenRankedRoutes,
  greenTimePercent,
  isVerifiedAllGreenRoute,
  routeCandidatesForMode,
  unknownPercent,
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
  reference,
  selected,
  recommended,
  fastest,
  greenest,
  rank,
  preview,
  onPreview,
  onSelect,
}: {
  route: RouteCandidate;
  reference?: RouteCandidate | undefined;
  selected: boolean;
  recommended: boolean;
  fastest: boolean;
  greenest: boolean;
  // Position in the green ranking, which is what the list is sorted by.
  rank?: number | undefined;
  preview?: boolean | undefined;
  onPreview?: (() => void) | undefined;
  onSelect: () => void;
}) {
  const { locale, t } = useLocale();
  const extraDistance = Math.max(0, route.distanceMeters - (reference?.distanceMeters ?? route.distanceMeters));
  const extraTime = Math.max(0, route.liveDurationSeconds - (reference?.liveDurationSeconds ?? route.liveDurationSeconds));
  const unknown = unknownPercent(route);
  const reasons = [...new Set([...route.reasonCodes, ...route.confidence.reasons])].slice(0, 3);

  return (
    <article className={`route-card ${selected ? "is-selected" : ""}`} data-testid={`route-card-${route.candidateId}`}>
      <div className="route-card-top">
        <div className="route-badges">
          {rank !== undefined && !fastest && <span className="badge badge-rank">#{rank}</span>}
          {recommended && <span className="badge badge-recommended">{t("recommended")}</span>}
          {fastest && <span className="badge">{t("fastest")}</span>}
          {greenest && !recommended && <span className="badge badge-green">{t("greenest")}</span>}
        </div>
        <span className={`confidence confidence-${route.confidence.level.toLowerCase()}`} title={`${t("confidence")}: ${Math.round(route.confidence.score * 100)}%`}>
          <span aria-hidden="true" />{t(`confidence_${route.confidence.level}`)} · {Math.round(route.confidence.score * 100)}%
        </span>
      </div>

      {/* The ranking criterion, on the thing being ranked. */}
      <button
        type="button"
        className={`route-green-share ${preview ? "is-active" : ""}`}
        onClick={onPreview}
        disabled={!onPreview}
        title={onPreview ? t("showOnMap") : undefined}
      >
        <strong>{t("greenShareTime", { value: greenTimePercent(route) })}</strong>
        <small>{t("greenShareDistance", { value: greenDistancePercent(route) })}</small>
      </button>

      <div className="route-primary-metrics">
        <div><ClockIcon /><span><strong>{formatDuration(route.liveDurationSeconds, locale)}</strong><small>{t("eta")}</small></span></div>
        <div><span className="metric-symbol">↔</span><span><strong>{formatDistance(route.distanceMeters, locale)}</strong><small>{t("distance")}</small></span></div>
        <div><span className="metric-symbol">+ </span><span><strong>{formatDuration(route.trafficDelaySeconds, locale)}</strong><small>{t("delay")}</small></span></div>
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
        <strong>{t("estimatedFree", { value: estimatedFreePercent(route) })}</strong>
        {unknown > 0 && <p className="unknown-note"><InfoIcon /> {t("unknownCoverage", { value: unknown })}</p>}
      </div>

      {(extraDistance > 0 || extraTime > 0) && (
        <p className="detour-note">{t("detour", { distance: formatDistance(extraDistance, locale), time: formatDuration(extraTime, locale) })}</p>
      )}
      {!extraDistance && !extraTime && <p className="detour-note">{t("noDetour")}</p>}

      {(route.tolls || route.unpaved) && (
        <div className="route-cautions">
          {route.tolls && <span><AlertIcon />{t("tolls")}</span>}
          {route.unpaved && <span><AlertIcon />{t("unpaved")}</span>}
        </div>
      )}

      <details className="route-reasons">
        <summary>{t("why")}</summary>
		<p className="route-explanation">{route.explanation}</p>
        <ul>{reasons.map((reason) => <li key={reason}>{reasonLabel(reason, locale)}</li>)}</ul>
      </details>

      <button className={`button ${selected ? "button-selected" : "button-secondary"}`} type="button" onClick={onSelect} disabled={selected}>
        {selected ? <><CheckIcon /> {t("selected")}</> : t("choose")}
      </button>
      <ExternalRouteLinks route={route} />
    </article>
  );
}

export function RouteResults({
  result,
  routingMode,
  selectedCandidateId,
  onSelect,
  previewCandidateId,
  onPreview,
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
  previewCandidateId?: string | undefined;
  onPreview?: ((candidateId: string) => void) | undefined;
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
  const routes = useMemo(() => routeCandidatesForMode(result, routingMode), [result, routingMode]);
  // Everything the search found, best green share first. In the strict mode the
  // list is what did not qualify, which is more use than an empty panel.
  const ranked = useMemo(() => greenRankedRoutes(result, 3), [result]);
  const measuredAt = useMemo(() => {
    const stamp = Date.parse(result.generatedAt);
    return Number.isFinite(stamp) ? new Date(stamp).toLocaleString(locale) : undefined;
  }, [result.generatedAt, locale]);
  const strictGreenUnavailable = routingMode === "STRICT_GREEN" && !isVerifiedAllGreenRoute(result.selectedRoute);
  const greenest = findGreenest(routes);
  const reference = result.fastestReferenceRoute ?? [...routes].sort((a, b) => a.liveDurationSeconds - b.liveDurationSeconds)[0];
  const recommendedId = result.selectedRoute?.candidateId;
  // Green share is the ranking this product is about, so it is the order of the
  // list; the reference route trails it as the thing being compared against.
  // Strict mode must never present the fast route as an answer, so when nothing
  // qualifies the fallback list shows the candidates without it (ADR-014).
  const listed = routes.length > 0
    ? routes
    : ranked.filter((route) => !strictGreenUnavailable || route.candidateId !== reference?.candidateId);
  const ordered = [...listed].sort((a, b) => {
    if (a.candidateId === reference?.candidateId) return 1;
    if (b.candidateId === reference?.candidateId) return -1;
    return greenTimePercent(b) - greenTimePercent(a) || a.liveDurationSeconds - b.liveDurationSeconds;
  });
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


      {strictGreenUnavailable && (
        <div className="notice notice-warning" role="status" data-testid="strict-green-empty">
          <AlertIcon />
          <div>
            <strong>{t("strictEmptyTitle")}</strong>
            <p>{t("strictEmptyBody")}</p>
          </div>
        </div>
      )}

      {!strictGreenUnavailable && degraded && (
        <div className="notice notice-warning" role="status"><AlertIcon /><div><strong>{t("degradedTitle")}</strong><p>{t("degradedBody")}</p></div></div>
      )}
      {!strictGreenUnavailable && result.warnings.length > 0 && (
        <div className="warning-list" aria-label={t("warningTitle")}>
          {result.warnings.map((warning, index) => (
            <p key={`${warning.code}-${index}`}><InfoIcon />{warningLabel(warning.code, warning.message, locale)}</p>
          ))}
        </div>
      )}

      <div className="route-list">
        {ordered.map((route, index) => (
          <RouteCard
            key={route.candidateId}
            route={route}
            reference={reference}
            selected={route.candidateId === selectedCandidateId}
            recommended={route.candidateId === recommendedId}
            fastest={route.candidateId === reference?.candidateId}
            greenest={route.candidateId === greenest?.candidateId}
            rank={index + 1}
            preview={previewCandidateId === route.candidateId}
            {...(onPreview ? { onPreview: () => onPreview(route.candidateId) } : {})}
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
