"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { ApiError, cancelRouteSearch, createRouteSearch, getRouteSearch } from "./api-client";
import { defaultCandidateId, displayedRoutes, displayedSelectedRoute } from "./route-insights";
import { clearCachedRouteResult, readCachedRouteResult, writeCachedRouteResult } from "./route-result-cache";
import {
  terminalStatuses,
  type RouteCandidate,
  type RouteSearchRequest,
  type RouteSearchResult,
  type RoutingMode,
  type SearchEvent,
} from "./schemas";
import { streamSearchEvents } from "./sse";

export type SearchTransport = "idle" | "sse" | "reconnecting" | "polling";

export type RouteSearchState = {
  phase: "idle" | "submitting" | "searching" | "ready" | "cancelled" | "error";
  result?: RouteSearchResult | undefined;
  routingMode?: RoutingMode | undefined;
  selectedCandidateId?: string | undefined;
  progress: number;
  // True when the result on screen came from the local cache rather than from a
  // search performed in this session.
  restored?: boolean | undefined;
  // The request that produced the result on screen, so it can be re-run.
  request?: RouteSearchRequest | undefined;
  eventType?: SearchEvent["type"] | undefined;
  transport: SearchTransport;
  reconnectAttempt: number;
  error?: {
    code: string;
    message: string;
    correlationId?: string | undefined;
    retryAfterSeconds?: number | undefined;
  } | undefined;
};

const initialState: RouteSearchState = {
  phase: "idle",
  progress: 0,
  transport: "idle",
  reconnectAttempt: 0,
};

function progressForEvent(event: SearchEvent): number {
  if (typeof event.progress === "number") return event.progress;
  return {
    SEARCH_ACCEPTED: 8,
    PROVIDER_REQUEST_STARTED: 18,
    QUEUED: 8,
    INITIAL_CANDIDATES_READY: 30,
    CANDIDATE_EVALUATED: 58,
    DETOUR_SEARCH_STARTED: 66,
    BETTER_ROUTE_FOUND: 82,
    SEARCH_COMPLETED: 100,
    SEARCH_DEGRADED: 100,
    SEARCH_FAILED: 100,
    BASELINE_EVALUATION_STARTED: 48,
    CANDIDATE_DISCOVERED: 65,
    SCORING_STARTED: 78,
    PROGRESS: 70,
    RESULT_UPDATED: 88,
    COMPLETED: 100,
    DEGRADED: 100,
    FAILED: 100,
    CANCELLED: 100,
    HEARTBEAT: 0,
  }[event.type];
}

function isTerminalEvent(type: SearchEvent["type"]): boolean {
  return ["COMPLETED", "DEGRADED", "FAILED", "CANCELLED", "SEARCH_COMPLETED", "SEARCH_DEGRADED", "SEARCH_FAILED"].includes(type);
}

function phaseForResult(result: RouteSearchResult): RouteSearchState["phase"] {
  if (result.status === "CANCELLED") return "cancelled";
  if (result.status === "FAILED" || result.status === "EXPIRED") return "error";
  if (terminalStatuses.has(result.status)) return "ready";
  return "searching";
}

function mergeCandidate(result: RouteSearchResult, candidate: RouteCandidate): RouteSearchResult {
  const alternatives = [...result.alternatives.filter((item) => item.candidateId !== candidate.candidateId), candidate];
  return { ...result, alternatives };
}

function abortableDelay(ms: number, signal: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    const timer = window.setTimeout(resolve, ms);
    signal.addEventListener(
      "abort",
      () => {
        window.clearTimeout(timer);
        reject(new DOMException("Aborted", "AbortError"));
      },
      { once: true },
    );
  });
}

function normalizedError(error: unknown): NonNullable<RouteSearchState["error"]> {
  if (error instanceof ApiError) {
    return {
      code: error.code,
      message: error.message,
      ...(error.correlationId ? { correlationId: error.correlationId } : {}),
      ...(error.retryAfterSeconds ? { retryAfterSeconds: error.retryAfterSeconds } : {}),
    };
  }
  return { code: "SEARCH_UNAVAILABLE", message: error instanceof Error ? error.message : "Search unavailable" };
}

function safeLocalStorage(): Storage | undefined {
  try {
    return window.localStorage;
  } catch {
    return undefined;
  }
}

export function useRouteSearch() {
  const [state, setState] = useState<RouteSearchState>(initialState);
  const controllerRef = useRef<AbortController | undefined>(undefined);
  const searchIdRef = useRef<string | undefined>(undefined);
  const generationRef = useRef(0);
  const selectionTouchedRef = useRef(false);

  // Restore the last completed analysis. Navigating away and back is not a new
  // question, and re-answering it would spend another provider request.
  useEffect(() => {
    const cached = readCachedRouteResult(safeLocalStorage(), Date.now());
    if (!cached) return;
    window.queueMicrotask(() => setState((current) => current.phase === "idle"
      ? {
        ...current,
        phase: "ready",
        result: cached.result,
        progress: 100,
        restored: true,
        ...(cached.request ? { request: cached.request } : {}),
        ...(cached.routingMode ? { routingMode: cached.routingMode } : {}),
        ...(cached.selectedCandidateId ? { selectedCandidateId: cached.selectedCandidateId } : {}),
      }
      : current));
  }, []);

  // Persist whenever a terminal result is on screen, including the selection so
  // the restored view is the one the user was actually looking at.
  useEffect(() => {
    if (state.phase !== "ready" || !state.result) return;
    writeCachedRouteResult(
      {
        result: state.result,
        routingMode: state.routingMode,
        selectedCandidateId: state.selectedCandidateId,
        request: state.request,
      },
      safeLocalStorage(),
      Date.now(),
    );
  }, [state.phase, state.result, state.routingMode, state.selectedCandidateId, state.request]);

  const monitorWithPolling = useCallback(async (searchId: string, signal: AbortSignal, deadline: number, generation: number) => {
    setState((current) => ({ ...current, transport: "polling", reconnectAttempt: 0 }));
    let failures = 0;
    while (!signal.aborted && Date.now() < deadline) {
      try {
        const result = await getRouteSearch(searchId, signal);
        if (generationRef.current !== generation) return;
        failures = 0;
        setState((current) => ({
          ...current,
          result,
          phase: phaseForResult(result),
          progress: terminalStatuses.has(result.status) ? 100 : Math.max(current.progress, 84),
          selectedCandidateId: selectionTouchedRef.current
            ? current.selectedCandidateId
            : defaultCandidateId(result, current.routingMode),
        }));
        if (terminalStatuses.has(result.status)) return;
      } catch (error) {
        if (signal.aborted) return;
        failures += 1;
        if (failures >= 3) throw error;
      }
      await abortableDelay(2_000, signal);
    }
    if (!signal.aborted) throw new ApiError("Search deadline exceeded", 504, "SEARCH_DEADLINE_EXCEEDED");
  }, []);

  const monitor = useCallback(async (
    searchId: string,
    signal: AbortSignal,
    deadline: number,
    generation: number,
  ) => {
    let terminalWithoutResult = false;
    try {
      await streamSearchEvents(
        searchId,
        signal,
        (event) => {
          if (generationRef.current !== generation) return true;
          const terminal = isTerminalEvent(event.type);
          terminalWithoutResult = terminal && !event.result;
          setState((current) => {
            const eventResult = event.result ?? (event.candidate && current.result ? mergeCandidate(current.result, event.candidate) : current.result);
            return {
              ...current,
              ...(eventResult ? { result: eventResult } : {}),
              phase: eventResult ? phaseForResult(eventResult) : terminal ? (event.type === "CANCELLED" ? "cancelled" : ["FAILED", "SEARCH_FAILED"].includes(event.type) ? "error" : "ready") : "searching",
              progress: Math.max(current.progress, progressForEvent(event)),
              eventType: event.type,
              transport: "sse",
              reconnectAttempt: 0,
              selectedCandidateId: selectionTouchedRef.current
                ? current.selectedCandidateId
                : eventResult ? defaultCandidateId(eventResult, current.routingMode) : current.selectedCandidateId,
            };
          });
          return terminal;
        },
        ({ attempt }) => {
          setState((current) => ({ ...current, transport: "reconnecting", reconnectAttempt: attempt }));
        },
      );

      if (terminalWithoutResult && !signal.aborted) {
        const result = await getRouteSearch(searchId, signal);
        if (generationRef.current === generation) {
          setState((current) => ({
            ...current,
            result,
            phase: phaseForResult(result),
            progress: 100,
            transport: "sse",
            selectedCandidateId: selectionTouchedRef.current
              ? current.selectedCandidateId
              : defaultCandidateId(result, current.routingMode),
          }));
        }
      }
    } catch (error) {
      if (signal.aborted || generationRef.current !== generation) return;
      try {
        await monitorWithPolling(searchId, signal, deadline, generation);
      } catch (pollingError) {
        if (signal.aborted || generationRef.current !== generation) return;
        setState((current) => ({ ...current, phase: "error", error: normalizedError(pollingError ?? error), transport: "idle" }));
      }
    }
  }, [monitorWithPolling]);

  const start = useCallback(async (request: RouteSearchRequest) => {
    controllerRef.current?.abort();
    const controller = new AbortController();
    controllerRef.current = controller;
    const generation = generationRef.current + 1;
    generationRef.current = generation;
    selectionTouchedRef.current = false;
    searchIdRef.current = undefined;
    setState({ phase: "submitting", routingMode: request.routingMode, progress: 4, transport: "idle", reconnectAttempt: 0, restored: false, request });

    try {
      const result = await createRouteSearch(request);
      if (controller.signal.aborted || generationRef.current !== generation) return;
      searchIdRef.current = result.searchId;
      const isTerminal = terminalStatuses.has(result.status);
      setState({
        phase: phaseForResult(result),
        result,
        routingMode: request.routingMode,
        selectedCandidateId: defaultCandidateId(result, request.routingMode),
        progress: isTerminal ? 100 : 12,
        transport: isTerminal ? "idle" : "sse",
        reconnectAttempt: 0,
      });
      if (!isTerminal) {
        void monitor(result.searchId, controller.signal, Date.now() + request.searchDeadlineMs + 20_000, generation);
      }
    } catch (error) {
      if (controller.signal.aborted || generationRef.current !== generation) return;
      setState({ phase: "error", progress: 0, transport: "idle", reconnectAttempt: 0, error: normalizedError(error) });
    }
  }, [monitor]);

  const cancel = useCallback(async () => {
    const searchId = searchIdRef.current;
    generationRef.current += 1;
    controllerRef.current?.abort();
    setState((current) => ({ ...current, phase: "cancelled", progress: 100, transport: "idle", eventType: "CANCELLED" }));
    if (!searchId) return;
    try {
      await cancelRouteSearch(searchId);
    } catch (error) {
      setState((current) => ({ ...current, error: normalizedError(error) }));
    }
  }, []);

  /**
   * Puts a previously analysed result back on screen. No provider request is
   * involved: the whole point of a bookmark is that the question was already
   * answered and paid for.
   */
  const restoreResult = useCallback((
    result: RouteSearchResult,
    routingMode?: RoutingMode,
    selectedCandidateId?: string,
  ) => {
    generationRef.current += 1;
    controllerRef.current?.abort();
    searchIdRef.current = result.searchId;
    selectionTouchedRef.current = Boolean(selectedCandidateId);
    setState({
      phase: "ready",
      result,
      progress: 100,
      transport: "idle",
      reconnectAttempt: 0,
      restored: true,
      ...(routingMode ? { routingMode } : {}),
      selectedCandidateId: selectedCandidateId ?? defaultCandidateId(result, routingMode),
    });
  }, []);

  const selectCandidate = useCallback((candidateId: string) => {
    selectionTouchedRef.current = true;
    setState((current) => {
      // Validated against what the interface actually offers. Checking the
      // mode filter instead meant every click was dropped in silence whenever
      // the filter qualified nothing — the card stayed on "Выбрать" for ever.
      if (!current.result || !displayedRoutes(current.result, current.routingMode).some((route) => route.candidateId === candidateId)) return current;
      return { ...current, selectedCandidateId: candidateId };
    });
  }, []);

  const reset = useCallback(() => {
    generationRef.current += 1;
    controllerRef.current?.abort();
    searchIdRef.current = undefined;
    selectionTouchedRef.current = false;
    clearCachedRouteResult(safeLocalStorage());
    setState(initialState);
  }, []);

  useEffect(() => () => controllerRef.current?.abort(), []);

  const selectedRoute = state.result
    ? displayedSelectedRoute(state.result, state.selectedCandidateId, state.routingMode)
    : undefined;

  return { state, start, cancel, reset, selectCandidate, restoreResult, selectedRoute };
}
