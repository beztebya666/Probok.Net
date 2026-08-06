"use client";

import { useEffect, useState } from "react";
import { useLocale } from "@/lib/i18n";
import {
  applyResolvedTheme,
  parseThemePreference,
  readThemePreference,
  resolveTheme,
  THEME_STORAGE_KEY,
  writeThemePreference,
  type ResolvedTheme,
  type ThemePreference,
} from "@/lib/theme";
import { MoonIcon, SunIcon } from "./icons";

function safeLocalStorage(): Storage | undefined {
  try {
    return window.localStorage;
  } catch {
    return undefined;
  }
}

function prefersDark(): boolean {
  return typeof window.matchMedia === "function" && window.matchMedia("(prefers-color-scheme: dark)").matches;
}

export function ThemeToggle() {
  const { t } = useLocale();
  const [preference, setPreference] = useState<ThemePreference>("system");
  // The inline bootstrap script already painted the right theme; this mirrors
  // its decision into React state without re-deciding it on the server.
  const [resolved, setResolved] = useState<ResolvedTheme>("light");

  useEffect(() => {
    const stored = readThemePreference(safeLocalStorage());
    const sync = (next: ThemePreference) => {
      setPreference(next);
      const theme = resolveTheme(next, prefersDark());
      setResolved(theme);
      applyResolvedTheme(theme);
    };
    window.queueMicrotask(() => sync(stored));

    const media = typeof window.matchMedia === "function" ? window.matchMedia("(prefers-color-scheme: dark)") : undefined;
    const onSystemChange = () => setPreference((current) => {
      if (current === "system") sync("system");
      return current;
    });
    const onStorage = (event: StorageEvent) => {
      if (event.key === THEME_STORAGE_KEY) sync(parseThemePreference(event.newValue));
    };
    media?.addEventListener("change", onSystemChange);
    window.addEventListener("storage", onStorage);
    return () => {
      media?.removeEventListener("change", onSystemChange);
      window.removeEventListener("storage", onStorage);
    };
  }, []);

  const toggle = () => {
    const next: ThemePreference = resolved === "dark" ? "light" : "dark";
    setPreference(next);
    setResolved(next);
    applyResolvedTheme(next);
    writeThemePreference(next, safeLocalStorage());
  };

  const label = resolved === "dark" ? t("themeToLight") : t("themeToDark");
  return (
    <button
      type="button"
      className="theme-toggle"
      onClick={toggle}
      aria-label={label}
      title={label}
      data-theme-preference={preference}
      data-testid="theme-toggle"
    >
      <span className="theme-toggle-track" aria-hidden="true">
        <SunIcon />
        <MoonIcon />
      </span>
    </button>
  );
}
