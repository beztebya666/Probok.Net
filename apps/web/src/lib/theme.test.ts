import { describe, expect, it } from "vitest";
import {
  applyResolvedTheme,
  parseThemePreference,
  readThemePreference,
  resolveTheme,
  THEME_STORAGE_KEY,
  themeBootstrapScript,
  writeThemePreference,
} from "./theme";

function memoryStorage(): Storage {
  const entries = new Map<string, string>();
  return {
    get length() {
      return entries.size;
    },
    clear: () => entries.clear(),
    getItem: (key: string) => entries.get(key) ?? null,
    key: (index: number) => [...entries.keys()][index] ?? null,
    removeItem: (key: string) => entries.delete(key),
    setItem: (key: string, value: string) => void entries.set(key, value),
  };
}

describe("theme preference", () => {
  it("round-trips an explicit choice and ignores anything unrecognised", () => {
    const storage = memoryStorage();
    writeThemePreference("dark", storage);
    expect(storage.getItem(THEME_STORAGE_KEY)).toBe("dark");
    expect(readThemePreference(storage)).toBe("dark");
    expect(parseThemePreference("sepia")).toBe("system");
    expect(parseThemePreference(null)).toBe("system");
    expect(readThemePreference(undefined)).toBe("system");
  });

  it("follows the system only until the user picks a side", () => {
    expect(resolveTheme("system", true)).toBe("dark");
    expect(resolveTheme("system", false)).toBe("light");
    expect(resolveTheme("light", true)).toBe("light");
    expect(resolveTheme("dark", false)).toBe("dark");
  });

  it("marks the document so CSS and native form controls agree on the theme", () => {
    const root = document.createElement("html");
    applyResolvedTheme("dark", root);
    expect(root.dataset.theme).toBe("dark");
    expect(root.style.colorScheme).toBe("dark");
  });

  it("ships a bootstrap script that reads the same key and cannot throw", () => {
    expect(themeBootstrapScript).toContain(JSON.stringify(THEME_STORAGE_KEY));
    expect(themeBootstrapScript).toContain("catch");
    expect(() => new Function(themeBootstrapScript)()).not.toThrow();
  });
});
