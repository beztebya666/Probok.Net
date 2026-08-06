"use client";

import { useEffect, useMemo, useState } from "react";
import { useLocale } from "@/lib/i18n";
import {
  parseTrafficProvider,
  initialTrafficProvider,
  readTrafficProvider,
  TRAFFIC_PROVIDER_STORAGE_KEY,
  writeTrafficProvider,
  type TrafficProvider,
} from "@/lib/route-presets";
import {
  canRefreshBookmark,
  isBookmarked,
  reconstructBookmarkRequest,
  parseRouteBookmarks,
  readRouteBookmarks,
  rememberRouteBookmark,
  removeRouteBookmark,
  ROUTE_BOOKMARKS_STORAGE_KEY,
  writeRouteBookmarks,
  type RouteBookmark,
} from "@/lib/route-bookmarks";
import { loadDemoPreload } from "@/lib/demo-preload";
import { seedDemoLists } from "@/lib/demo-seed";
import { readRoutePreferences, writeRoutePreferences } from "@/lib/route-preferences";
import { getRuntimeConfig } from "@/lib/runtime-config";
import type { RouteSearchRequest, RouteSearchResult, RoutingMode } from "@/lib/schemas";
import { useRouteSearch } from "@/lib/use-route-search";
import { AlertIcon, InfoIcon } from "./icons";
import type { LocationValue } from "./location-field";
import { AppHeader } from "./app-header";
import { DemoToast } from "./demo-toast";
import { RouteForm } from "./route-form";
import { RouteMap } from "./route-map";
import { RouteResults } from "./route-results";
import { SearchProgress } from "./search-progress";

function safeLocalStorage(): Storage | undefined {
  try {
    return window.localStorage;
  } catch {
    return undefined;
  }
}

export function GreenRouteApp() {
  const config = useMemo(() => getRuntimeConfig(), []);
  const { t } = useLocale();
  const { state, start, cancel, reset, selectCandidate, restoreResult, selectedRoute } = useRouteSearch();
  const [trafficProvider, setTrafficProvider] = useState<TrafficProvider>("off");
  const [bookmarks, setBookmarks] = useState<RouteBookmark[]>([]);
  const [routeLabel, setRouteLabel] = useState("");
  // Counts searches rather than flags one, so a second search re-announces the
  // demo instead of being swallowed by a toast that is already fading.
  const [demoNotices, setDemoNotices] = useState(0);
  // What the planner should show: set when an analysis is opened from a saved
  // list, so the form matches the result on screen instead of sitting empty.
  const [plannerRoute, setPlannerRoute] = useState<{ key: string; origin: LocationValue; destination: LocationValue } | undefined>(undefined);
  const [lastRequest, setLastRequest] = useState<RouteSearchRequest | undefined>(undefined);
  // While a bookmark is being refreshed we hold its previous content, so a
  // provider outage or an exhausted quota restores it instead of losing it.
  const [refreshing, setRefreshing] = useState<{
    bookmark?: RouteBookmark | undefined;
    request: RouteSearchRequest;
    result: RouteSearchResult;
    routingMode?: RoutingMode | undefined;
    selectedCandidateId?: string | undefined;
  } | undefined>(undefined);
  const busy = state.phase === "submitting" || state.phase === "searching";
  const searchId = state.result?.searchId;

  useEffect(() => {
    let active = true;
    let storage: Storage | undefined;
    try {
      storage = window.localStorage;
    } catch {
      storage = undefined;
    }
    const initial = initialTrafficProvider(storage, Boolean(config.twoGisMapGLBrowserKey));
    window.queueMicrotask(() => {
      if (active) setTrafficProvider(initial);
    });
    const syncStorage = (event: StorageEvent) => {
      if (event.key === TRAFFIC_PROVIDER_STORAGE_KEY) setTrafficProvider(parseTrafficProvider(event.newValue));
    };
    window.addEventListener("storage", syncStorage);
    return () => {
      active = false;
      window.removeEventListener("storage", syncStorage);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // A demo that opens empty looks broken. The shipped analysis fills the first
  // screen; a visitor who already searched something keeps their own result.
  useEffect(() => {
    if (!config.demoMode || state.result || state.phase !== "idle") return;
    const controller = new AbortController();
    void loadDemoPreload(controller.signal).then((preload) => {
      if (!preload || controller.signal.aborted) return;
      setLastRequest(preload.request);
      // Favourites, recent searches and saved analyses are features nobody can
      // see when all three tabs are empty.
      seedDemoLists(preload, safeLocalStorage(), Date.now());
      setBookmarks(readRouteBookmarks(safeLocalStorage()));
      setTrafficProvider(readTrafficProvider(safeLocalStorage()));
      if (preload.origin && preload.destination) {
        setRouteLabel(`${preload.origin} → ${preload.destination}`);
        setPlannerRoute({
          key: "demo",
          origin: { label: preload.origin, point: preload.request.origin },
          destination: { label: preload.destination, point: preload.request.destination },
        });
      }
      restoreResult(preload.result, preload.request.routingMode, preload.result.greenTopRoutes[0]?.candidateId);
    });
    return () => controller.abort();
    // Runs once: after the first restore state.result is set and the guard holds.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [config.demoMode]);

  useEffect(() => {
    let active = true;
    const storage = safeLocalStorage();
    const initial = readRouteBookmarks(storage);
    window.queueMicrotask(() => {
      if (active) setBookmarks(initial);
    });
    const syncStorage = (event: StorageEvent) => {
      if (event.key === ROUTE_BOOKMARKS_STORAGE_KEY) setBookmarks(parseRouteBookmarks(event.newValue));
    };
    window.addEventListener("storage", syncStorage);
    return () => {
      active = false;
      window.removeEventListener("storage", syncStorage);
    };
  }, []);

  const persistBookmarks = (update: (current: RouteBookmark[]) => RouteBookmark[]) => {
    setBookmarks((current) => {
      const next = update(current);
      writeRouteBookmarks(next, safeLocalStorage());
      return next;
    });
  };

  // A refresh is an ordinary search under the hood: it either produces a newer
  // analysis, or it fails and the saved one is put back untouched.
  useEffect(() => {
    if (!refreshing) return;
    const settled = state.phase === "ready" && state.result && !state.restored;
    const failed = state.phase === "error" || state.phase === "cancelled";
    if (!settled && !failed) return;
    window.queueMicrotask(() => {
      if (settled && refreshing.bookmark) {
        persistBookmarks((current) => rememberRouteBookmark(current, {
          ...refreshing.bookmark!,
          savedAt: Date.now(),
          // Storing the request that just ran turns a reconstructed refresh of a
          // legacy record into an exact one from now on.
          request: refreshing.request,
          result: state.result!,
          ...(state.routingMode ? { routingMode: state.routingMode } : {}),
          ...(state.selectedCandidateId ? { selectedCandidateId: state.selectedCandidateId } : {}),
        }));
      } else if (failed) {
        // The search failed, so the analysis that was on screen goes back
        // untouched instead of leaving an empty pane.
        restoreResult(refreshing.result, refreshing.routingMode, refreshing.selectedCandidateId);
      }
      setRefreshing(undefined);
    });
  }, [refreshing, state.phase, state.result, state.restored, state.routingMode, state.selectedCandidateId, restoreResult]);

  const openedBookmark = bookmarks.find((entry) => entry.result.searchId === searchId);
  const refreshableRequest = state.request ?? lastRequest;
  // An opened bookmark answers for itself; only a live search falls back to the
  // request the form last submitted.
  const canRefresh = openedBookmark ? canRefreshBookmark(openedBookmark) : Boolean(refreshableRequest);

  /**
   * Re-asks the same question. The analysis currently on screen is held aside
   * first, so a provider error or an exhausted quota puts it back rather than
   * clearing the pane.
   */
  /**
   * Opening a saved analysis restores the whole search, not just its result:
   * addresses, mode and allowances go back into the planner so the panel and
   * the form describe the same trip.
   */
  const openBookmark = (bookmark: RouteBookmark) => {
    restoreResult(bookmark.result, bookmark.routingMode, bookmark.selectedCandidateId);
    const storage = safeLocalStorage();
    const request = reconstructBookmarkRequest(bookmark, readRoutePreferences(storage));
    if (!request) return;
    setLastRequest(request);
    writeRoutePreferences({
      routingMode: request.routingMode,
      extraDistanceKm: Math.round(request.maxExtraDistanceMeters / 1000),
      extraTimeMinutes: Math.round(request.maxExtraTimeSeconds / 60),
      avoidTolls: request.avoidTolls,
      avoidUnpaved: request.avoidUnpaved,
    }, storage);
    const [origin = "", destination = ""] = bookmark.label.split("→").map((part) => part.trim());
    setPlannerRoute({
      key: `bookmark-${bookmark.id}`,
      origin: { label: origin, point: request.origin },
      destination: { label: destination, point: request.destination },
    });
  };

  const announceDemo = () => {
    if (config.demoMode) setDemoNotices((current) => current + 1);
  };

  const refreshCurrent = (bookmark?: RouteBookmark) => {
    // Refreshing a bookmark re-asks *its* question, not whatever is on screen.
    // Records saved before requests were stored get one rebuilt from the
    // analysed endpoints, so age is no longer a reason to lose the button.
    const target = bookmark ?? openedBookmark;
    const request = target
      ? reconstructBookmarkRequest(target, readRoutePreferences(safeLocalStorage()))
      : refreshableRequest;
    // A rollback needs something to roll back to: the analysis on screen, or
    // the bookmark's own saved one when the refresh starts from the list.
    const snapshot = state.result ?? bookmark?.result;
    const fromScreen = Boolean(state.result);
    const routingMode = fromScreen ? state.routingMode : bookmark?.routingMode;
    const selectedCandidateId = fromScreen ? state.selectedCandidateId : bookmark?.selectedCandidateId;
    if (!request || !snapshot) return;
    announceDemo();
    const started = { ...request, requestId: crypto.randomUUID(), departureTime: new Date().toISOString() };
    setRefreshing({
      ...(target ? { bookmark: target } : {}),
      request: started,
      result: snapshot,
      ...(routingMode ? { routingMode } : {}),
      ...(selectedCandidateId ? { selectedCandidateId } : {}),
    });
    void start(started);
  };

  // One control, both directions: the icon is the state and the switch for it.
  const toggleBookmark = () => {
    if (!state.result) return;
    const existing = bookmarks.find((entry) => entry.result.searchId === state.result!.searchId);
    if (existing) {
      persistBookmarks((current) => removeRouteBookmark(current, existing.id));
      return;
    }
    persistBookmarks((current) => rememberRouteBookmark(current, {
      id: `${state.result!.searchId}`,
      label: routeLabel || t("bookmarksTitle"),
      savedAt: Date.now(),
      result: state.result!,
      ...(lastRequest ? { request: lastRequest } : {}),
      ...(state.routingMode ? { routingMode: state.routingMode } : {}),
      ...(state.selectedCandidateId ? { selectedCandidateId: state.selectedCandidateId } : {}),
    }));
  };

  const changeTrafficProvider = (provider: TrafficProvider) => {
    setTrafficProvider(provider);
    let storage: Storage | undefined;
    try {
      storage = window.localStorage;
    } catch {
      storage = undefined;
    }
    writeTrafficProvider(provider, storage);
  };

  return (
    <div className="app-shell">
      <AppHeader demoMode={config.demoMode} />
      <DemoToast trigger={demoNotices} />

      <main className={`workspace ${state.result ? "workspace-with-results" : ""}`} id="main-content">
        <aside className="planner-panel surface-panel" aria-label={t("plannerTitle")}>
          {!config.configured && (
            <div className="notice notice-error" role="alert">
              <AlertIcon /><div><strong>{t("configTitle")}</strong><p>{t("configBody")}</p></div>
            </div>
          )}
          <RouteForm
            // The preload arrives after the first render, and initial state is
            // only read once; remounting is what makes it visible.
            {...(plannerRoute ? { initialRoute: plannerRoute } : {})}
            onSubmit={(request) => { setLastRequest(request); setRefreshing(undefined); announceDemo(); return start(request); }}
            onRouteLabelChange={setRouteLabel}
            bookmarks={bookmarks}
            activeSearchId={searchId}
            onOpenBookmark={openBookmark}
            onRemoveBookmark={(id) => persistBookmarks((current) => removeRouteBookmark(current, id))}
            busy={busy}
            configured={config.configured}
            trafficProvider={trafficProvider}
            onTrafficProviderChange={changeTrafficProvider}
            status={
              <>
                {busy && <SearchProgress state={state} onCancel={() => void cancel()} />}
                {state.phase === "cancelled" && (
                  <div className="notice" role="status"><InfoIcon /><div><strong>{t("cancel")}</strong><p>{t("retry")}</p></div></div>
                )}
                {state.phase === "error" && state.error && (
                  <div className="notice notice-error" role="alert">
                    <AlertIcon />
                    <div>
                      <strong>{t("searchError")}</strong>
                      <p>{state.error.message}</p>
                      {state.error.correlationId && <small>{t("correlationId", { value: state.error.correlationId })}</small>}
                      <button type="button" className="text-button" onClick={reset}>{t("retry")}</button>
                    </div>
                  </div>
                )}
              </>
            }
          />
        </aside>

        <RouteMap
          result={state.result}
          routingMode={state.routingMode}
          selectedRoute={selectedRoute}
          trafficProvider={trafficProvider}
          onTrafficProviderChange={changeTrafficProvider}
          browserKey={config.yandexMapsBrowserKey}
          twoGisBrowserKey={config.twoGisMapGLBrowserKey}
          twoGisDarkStyleId={config.twoGisMapGLDarkStyleId}
          yandexTrafficAvailable={config.yandexTrafficAvailable}
            demoMode={config.demoMode}
        />

        {state.result && (
          <aside className="results-panel surface-panel" aria-label={t("resultTitle")}>
            <RouteResults
              result={state.result}
              routingMode={state.routingMode}
              // The highlighted card is the route on the map, always.
              selectedCandidateId={selectedRoute?.candidateId}
              onSelect={selectCandidate}
              bookmarked={isBookmarked(bookmarks, searchId)}
              onSaveBookmark={toggleBookmark}
              onRefresh={canRefresh ? () => refreshCurrent() : undefined}
              refreshing={Boolean(refreshing)}
              onDismiss={reset}
            />
          </aside>
        )}
      </main>
    </div>
  );
}
