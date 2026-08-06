export const THEME_STORAGE_KEY = "greenroute.theme.v1";

export type ThemePreference = "system" | "light" | "dark";
export type ResolvedTheme = "light" | "dark";

export function parseThemePreference(raw: string | null): ThemePreference {
  return raw === "light" || raw === "dark" || raw === "system" ? raw : "system";
}

export function readThemePreference(storage?: Pick<Storage, "getItem">): ThemePreference {
  if (!storage) return "system";
  try {
    return parseThemePreference(storage.getItem(THEME_STORAGE_KEY));
  } catch {
    return "system";
  }
}

export function writeThemePreference(preference: ThemePreference, storage?: Pick<Storage, "setItem">): void {
  if (!storage) return;
  try {
    storage.setItem(THEME_STORAGE_KEY, preference);
  } catch {
    // Private browsing and storage quotas can make localStorage unavailable.
  }
}

export function resolveTheme(preference: ThemePreference, prefersDark: boolean): ResolvedTheme {
  if (preference === "system") return prefersDark ? "dark" : "light";
  return preference;
}

/**
 * Applied by an inline script before first paint and again on every change, so
 * the document never renders one theme and then swaps to the other.
 */
export function applyResolvedTheme(theme: ResolvedTheme, root?: HTMLElement): void {
  const element = root ?? (typeof document === "undefined" ? undefined : document.documentElement);
  if (!element) return;
  element.dataset.theme = theme;
  element.style.colorScheme = theme;
}

/**
 * Runs before hydration, inlined into <head>. It is deliberately tiny and
 * dependency-free: anything it cannot read falls back to the light theme that
 * the stylesheet already declares.
 */
export const themeBootstrapScript = `(function(){try{var p=localStorage.getItem(${JSON.stringify(THEME_STORAGE_KEY)});var d=window.matchMedia&&window.matchMedia("(prefers-color-scheme: dark)").matches;var t=(p==="light"||p==="dark")?p:(d?"dark":"light");document.documentElement.dataset.theme=t;document.documentElement.style.colorScheme=t;}catch(e){}})();`;
