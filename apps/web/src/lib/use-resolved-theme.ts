"use client";

import { useEffect, useState } from "react";
import type { ResolvedTheme } from "./theme";

/**
 * Reads the theme the inline bootstrap script committed to the document and
 * follows later changes. Components observe the DOM instead of receiving the
 * theme through props, so a theme switch never has to be threaded through every
 * layer that happens to sit between the toggle and the map.
 */
export function useResolvedTheme(): ResolvedTheme {
  const [theme, setTheme] = useState<ResolvedTheme>("light");

  useEffect(() => {
    const read = () => setTheme(document.documentElement.dataset.theme === "dark" ? "dark" : "light");
    window.queueMicrotask(read);
    const observer = new MutationObserver(read);
    observer.observe(document.documentElement, { attributeFilter: ["data-theme"] });
    return () => observer.disconnect();
  }, []);

  return theme;
}
