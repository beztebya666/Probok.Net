"use client";

import { useLocale } from "@/lib/i18n";
import type { RouteSearchState } from "@/lib/use-route-search";
import { XIcon } from "./icons";

function progressKey(type: RouteSearchState["eventType"]): "progressQueued" | "progressInitial" | "progressBaseline" | "progressCandidate" | "progressScoring" | "progressFinal" | "progressDone" {
  if (type === "INITIAL_CANDIDATES_READY") return "progressInitial";
  if (type === "BASELINE_EVALUATION_STARTED" || type === "CANDIDATE_EVALUATED") return "progressBaseline";
  if (type === "CANDIDATE_DISCOVERED" || type === "BETTER_ROUTE_FOUND") return "progressCandidate";
  if (type === "DETOUR_SEARCH_STARTED") return "progressCandidate";
  if (type === "SCORING_STARTED") return "progressScoring";
  if (type === "RESULT_UPDATED" || type === "PROGRESS") return "progressFinal";
  if (["COMPLETED", "DEGRADED", "SEARCH_COMPLETED", "SEARCH_DEGRADED"].includes(type ?? "")) return "progressDone";
  return "progressQueued";
}

export function SearchProgress({ state, onCancel }: { state: RouteSearchState; onCancel: () => void }) {
  const { t } = useLocale();
  const transport = state.transport === "reconnecting" ? t("reconnecting") : state.transport === "polling" ? t("polling") : undefined;
  return (
    <section className="progress-card" aria-live="polite" aria-busy="true">
      <div className="progress-header"><div><p className="eyebrow">{t("searching")}</p><strong>{t(progressKey(state.eventType))}</strong></div><span>{Math.round(state.progress)}%</span></div>
      <div className="progress-track" role="progressbar" aria-valuemin={0} aria-valuemax={100} aria-valuenow={Math.round(state.progress)}>
        <span style={{ width: `${state.progress}%` }} />
      </div>
      <div className="progress-footer">
        <span>{transport ?? "SSE · live"}</span>
        <button className="text-button danger" type="button" onClick={onCancel}><XIcon /> {t("cancelSearch")}</button>
      </div>
    </section>
  );
}
