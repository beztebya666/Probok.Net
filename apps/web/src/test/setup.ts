import "@testing-library/jest-dom/vitest";

Object.defineProperty(window, "matchMedia", {
  writable: true,
  value: (query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: () => undefined,
    removeListener: () => undefined,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
    dispatchEvent: () => false,
  }),
});

// jsdom has no layout, so it ships no ResizeObserver. The keyless map measures
// its container with one; a stub that never fires keeps it at zero size, which
// is exactly what a headless test should see.
class NoopResizeObserver implements ResizeObserver {
  observe(): void {}
  unobserve(): void {}
  disconnect(): void {}
}

Object.defineProperty(window, "ResizeObserver", { writable: true, value: NoopResizeObserver });
globalThis.ResizeObserver = NoopResizeObserver;
