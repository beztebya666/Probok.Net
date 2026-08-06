"use client";

import { useEffect, useState } from "react";
import { useLocale } from "@/lib/i18n";
import { InfoIcon } from "./icons";

const VISIBLE_MS = 4200;

/**
 * Says out loud that a search in the demo build answered from fixtures.
 *
 * The demo is indistinguishable from the real thing while you are using it —
 * routes appear, percentages differ, the map is live — so pressing "find" has to
 * state plainly that nothing was asked of a provider.
 *
 * `trigger` is a counter rather than a boolean so that repeated searches restart
 * the toast instead of leaving the first one to fade out on its own.
 */
export function DemoToast({ trigger }: { trigger: number }) {
  const { t } = useLocale();
  // Visibility is derived, not stored: the only state is which search has
  // already been announced, which keeps the effect to a single timer.
  const [announced, setAnnounced] = useState(0);

  useEffect(() => {
    if (trigger === 0) return;
    const timer = window.setTimeout(() => setAnnounced(trigger), VISIBLE_MS);
    return () => window.clearTimeout(timer);
  }, [trigger]);

  if (trigger === 0 || announced >= trigger) return null;
  return (
    <div className="demo-toast" role="status" aria-live="polite">
      <InfoIcon aria-hidden="true" />
      <span>{t("demoToast")}</span>
    </div>
  );
}
