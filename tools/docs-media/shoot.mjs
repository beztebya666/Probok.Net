/**
 * Renders the README screenshots from the saved demo analysis.
 *
 * Point it at a demo-mode server that has a 2GIS MapGL browser key, so the map
 * tiles are real while the analysis comes from the fixture:
 *
 *   node tools/docs-media/shoot.mjs http://127.0.0.1:3200 docs/media
 */
import { chromium } from "playwright-core";
import { readFileSync, mkdirSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join, resolve } from "node:path";

const BASE = process.argv[2] ?? "http://127.0.0.1:3200";
const OUT = resolve(process.argv[3] ?? "docs/media");
const here = dirname(fileURLToPath(import.meta.url));
const fixture = JSON.parse(readFileSync(join(here, "demo-analysis.json"), "utf8"));
mkdirSync(OUT, { recursive: true });

const SAVED = JSON.stringify({
  version: 1,
  savedAt: Date.parse(fixture.result.generatedAt),
  routingMode: "GREENEST",
  selectedCandidateId: fixture.result.selectedRoute.candidateId,
  request: fixture.request,
  result: fixture.result,
});
const BOOKMARKS = JSON.stringify({
  version: 1,
  entries: [{
    id: fixture.result.searchId,
    label: "Кутузовский проспект, 32 → улица Академика Королёва, 12",
    savedAt: Date.parse(fixture.result.generatedAt),
    routingMode: "GREENEST",
    selectedCandidateId: fixture.result.selectedRoute.candidateId,
    request: fixture.request,
    result: fixture.result,
  }],
});
const PREFERENCES = JSON.stringify({
  version: 1,
  preferences: { routingMode: "GREENEST", extraDistanceKm: 150, extraTimeMinutes: 300, avoidTolls: false, avoidUnpaved: true },
});
const PRESETS = JSON.stringify({
  version: 1,
  entries: [
    {
      id: "preset-favourite",
      origin: { label: "Кутузовский проспект, 32", point: fixture.request.origin },
      destination: { label: "улица Академика Королёва, 12", point: fixture.request.destination },
      waypoints: [],
      routingMode: "GREENEST",
      extraDistanceKm: 150,
      extraTimeMinutes: 300,
      avoidTolls: false,
      avoidUnpaved: true,
      trafficProvider: "2gis",
      favorite: true,
      usedAt: Date.parse(fixture.result.generatedAt),
    },
  ],
});

const browser = await chromium.launch({ channel: "chromium" });

async function open({ width, height, theme, scale = 2, waitMs = 6500 }) {
  const context = await browser.newContext({
    viewport: { width, height },
    deviceScaleFactor: scale,
    locale: "ru-RU",
    timezoneId: "Europe/Moscow",
    isMobile: width < 520,
    hasTouch: width < 520,
  });
  await context.addInitScript(
    ([theme, saved, bookmarks, presets, preferences]) => {
      window.localStorage.setItem("greenroute.theme.v1", theme);
      window.localStorage.setItem("greenroute.traffic-provider.v1", JSON.stringify({ version: 1, provider: "2gis" }));
      window.localStorage.setItem("greenroute.last-result.v1", saved);
      window.localStorage.setItem("greenroute.bookmarks.v1", bookmarks);
      window.localStorage.setItem("greenroute.route-presets.v1", presets);
      window.localStorage.setItem("greenroute.route-preferences.v1", preferences);
    },
    [theme, SAVED, BOOKMARKS, PRESETS, PREFERENCES],
  );
  const page = await context.newPage();
  await page.goto(BASE, { waitUntil: "domcontentloaded" });
  await page.locator('html[data-greenroute-hydrated="true"]').waitFor();
  // The restored analysis does not refill the planner, and an empty pair of
  // address fields reads as "nothing has happened yet" in a screenshot.
  await page.getByRole("combobox", { name: "Откуда", exact: true }).fill("Кутузовский проспект, 32");
  await page.keyboard.press("Escape");
  await page.getByRole("combobox", { name: "Куда", exact: true }).fill("улица Академика Королёва, 12");
  await page.keyboard.press("Escape");
  await page.locator("body").click({ position: { x: 2, y: 2 } });
  await page.waitForTimeout(waitMs);
  return { context, page };
}

async function shoot(name, options, action) {
  const { context, page } = await open(options);
  const target = action ? await action(page) : page;
  await page.waitForTimeout(600);
  await target.screenshot({ path: join(OUT, `${name}.png`), animations: "disabled" });
  console.log(`✓ ${name}.png`);
  await context.close();
}

for (const theme of ["light", "dark"]) {
  await shoot(`desktop-${theme}`, { width: 1440, height: 880, theme });
  await shoot(`mobile-${theme}`, { width: 430, height: 932, theme, scale: 3, waitMs: 7000 });
  await shoot(`podium-${theme}`, { width: 1440, height: 880, theme }, async (page) => page.locator(".green-podium"));
  await shoot(`planner-${theme}`, { width: 1440, height: 880, theme }, async (page) => page.locator(".planner-panel"));
}

await shoot("tablet-light", { width: 1100, height: 1000, theme: "light" });

await shoot("bookmarks-light", { width: 1440, height: 880, theme: "light" }, async (page) => {
  await page.getByTestId("preset-tab-bookmarks").click();
  await page.waitForTimeout(400);
  return page.locator(".route-presets");
});

await browser.close();
