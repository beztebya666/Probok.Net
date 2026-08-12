/**
 * Renders the screenshot set for the presentation pack.
 *
 *   node tools/docs-media/shoot-pack.mjs <base-url> <out-dir>
 *
 * Shoots the live stack, so the map is the provider's with its traffic layer
 * and the analysis is the frozen one a saved search restores. Output is PNG at
 * two device pixels per CSS pixel: a deck projects these full screen, where the
 * WebP the README uses would show its compression.
 *
 * Every window shot is clipped to the last route card that fits, and every
 * panel shot is taken from the element itself, so nothing lands half cut.
 */
import { createRequire } from "node:module";
import { readFileSync, mkdirSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join, resolve } from "node:path";

// The browser lives in the web app's dependencies, and ESM resolves imports
// from this file's directory, so it is asked for by path rather than by name.
const { chromium } = createRequire(new URL("../../apps/web/package.json", import.meta.url))("playwright-core");

const BASE = process.argv[2] ?? "http://localhost:3000";
const OUT = resolve(process.argv[3] ?? "tmp/FOR_INVESTORS/screenshots");
const here = dirname(fileURLToPath(import.meta.url));
// Which analysis to shoot: a live capture for the pack, the demo fixture by
// default.
const fixture = JSON.parse(readFileSync(process.env.PACK_FIXTURE ?? join(here, "demo-analysis.json"), "utf8"));
mkdirSync(OUT, { recursive: true });

const saved = (mode) => JSON.stringify({
  version: 1,
  savedAt: Date.parse(fixture.result.generatedAt),
  routingMode: mode ?? fixture.request.routingMode,
  // The interface opens on what the orchestrator recommends, so the badge and
  // the selection agree; picking the greenest here would show a card marked
  // "recommended" while a different one is chosen.
  selectedCandidateId: (fixture.result.selectedRoute ?? fixture.result.greenTopRoutes?.[0])?.candidateId,
  request: mode ? { ...fixture.request, routingMode: mode } : fixture.request,
  result: fixture.result,
});

const NOW = Date.parse(fixture.result.generatedAt);
const HOUR = 60 * 60 * 1000;
const base = {
  waypoints: [],
  routingMode: fixture.request.routingMode,
  extraDistanceKm: Math.round(fixture.request.maxExtraDistanceMeters / 1000),
  extraTimeMinutes: Math.round(fixture.request.maxExtraTimeSeconds / 60),
  avoidTolls: fixture.request.avoidTolls,
  avoidUnpaved: fixture.request.avoidUnpaved,
  trafficProvider: "2gis",
};
const trip = {
  origin: { label: fixture.labels.origin, point: fixture.request.origin },
  destination: { label: fixture.labels.destination, point: fixture.request.destination },
};
const PRESETS = JSON.stringify({
  version: 1,
  entries: [
    { ...base, ...trip, id: "pack-favourite", favorite: true, lastUsedAt: NOW },
    { ...base, origin: trip.destination, destination: trip.origin, id: "pack-return", favorite: false, lastUsedAt: NOW - 2 * HOUR },
    {
      ...base,
      origin: { label: "Московский Кремль", point: { latitude: 55.75222, longitude: 37.61556 } },
      destination: { label: "ВДНХ", point: { latitude: 55.8299, longitude: 37.6331 } },
      id: "pack-vdnh",
      favorite: false,
      lastUsedAt: NOW - 24 * HOUR,
    },
  ],
});
const PREFERENCES = (mode) => JSON.stringify({
  version: 1,
  preferences: {
    routingMode: mode ?? fixture.request.routingMode,
    extraDistanceKm: Math.round(fixture.request.maxExtraDistanceMeters / 1000),
    extraTimeMinutes: Math.round(fixture.request.maxExtraTimeSeconds / 60),
    avoidTolls: fixture.request.avoidTolls,
    avoidUnpaved: fixture.request.avoidUnpaved,
  },
});
const analyses = [fixture, ...(fixture.extra ?? [])];
const BOOKMARKS = JSON.stringify({
  version: 1,
  entries: analyses.map((analysis, index) => ({
    id: analysis.result.searchId,
    label: `${analysis.labels.origin} → ${analysis.labels.destination}`,
    savedAt: NOW - index * HOUR,
    routingMode: analysis.request.routingMode,
    result: analysis.result,
    request: analysis.request,
    ...((analysis.result.selectedRoute ?? analysis.result.greenTopRoutes?.[0])
      ? { selectedCandidateId: (analysis.result.selectedRoute ?? analysis.result.greenTopRoutes[0]).candidateId }
      : {}),
  })),
});

const browser = await chromium.launch({ channel: "chromium" });

async function open(theme, size, { scale = 2, mode, locale = "ru" } = {}) {
  const context = await browser.newContext({
    viewport: size,
    deviceScaleFactor: scale,
    locale: locale === "ru" ? "ru-RU" : "en-US",
    timezoneId: "Europe/Moscow",
    isMobile: size.width < 520,
    hasTouch: size.width < 520,
  });
  await context.addInitScript(([theme, restored, locale, presets, bookmarks]) => {
    window.localStorage.setItem("greenroute.theme.v1", theme);
    window.localStorage.setItem("greenroute.locale", locale);
    window.localStorage.setItem("greenroute.traffic-provider.v1", JSON.stringify({ version: 1, provider: "2gis" }));
    window.localStorage.setItem("greenroute.last-result.v1", restored);
    // A fresh browser profile has empty tabs, and empty tabs photograph as a
    // missing feature rather than an unused one.
    window.localStorage.setItem("greenroute.route-presets.v1", presets);
    window.localStorage.setItem("greenroute.bookmarks.v1", bookmarks);
    // The form must say what the shown analysis was asked for; a panel reading
    // one mode beside results produced by another is a picture of a bug.
    window.localStorage.setItem("greenroute.route-preferences.v1", preferences);
  }, [theme, saved(mode), locale, PRESETS, BOOKMARKS, PREFERENCES(mode)]);
  const page = await context.newPage();
  await page.goto(BASE, { waitUntil: "domcontentloaded" });
  await page.locator('html[data-greenroute-hydrated="true"]').waitFor({ timeout: 30_000 });
  // A restored analysis does not refill the address fields, and empty fields
  // above a finished search photograph as a broken screen. The labels are
  // typed, not chosen, so no geocoder request is spent on them.
  const fill = async (name, value) => {
    const field = page.getByRole("combobox", { name, exact: true });
    if (await field.count() === 0) return;
    await field.fill(value);
    await page.keyboard.press("Escape");
  };
  await fill(locale === "ru" ? "Откуда" : "From", fixture.labels.origin);
  await fill(locale === "ru" ? "Куда" : "To", fixture.labels.destination);
  await page.locator("body").click({ position: { x: 2, y: 2 } });
  // The provider's custom dark style arrives after the default one, and a
  // screenshot taken in between catches an unstyled basemap that looks like a
  // broken map rather than a slow one.
  await page.waitForTimeout(13_500);
  return { context, page };
}

/** Height at which the window can be cut without slicing a card in half. */
async function cleanHeight(page, fallback) {
  return page.evaluate((fallback) => {
    const cards = [...document.querySelectorAll('[data-testid^="route-card-"]')];
    const fitting = cards.map((card) => card.getBoundingClientRect().bottom).filter((bottom) => bottom <= fallback);
    const last = fitting.at(-1);
    return last ? Math.ceil(last + 16) : fallback;
  }, fallback);
}

async function window_(page, name, size) {
  const height = await cleanHeight(page, size.height);
  await page.screenshot({ path: `${OUT}/${name}.png`, clip: { x: 0, y: 0, width: size.width, height } });
  console.log(`✓ ${name}`);
}

async function element(page, selector, name) {
  const target = page.locator(selector).first();
  if (await target.count() === 0) return console.log(`· ${name} (absent)`);
  await target.screenshot({ path: `${OUT}/${name}.png` });
  console.log(`✓ ${name}`);
}

const DESKTOP = { width: 1600, height: 1000 };

// 1-2. The whole product in one frame, in both themes.
for (const theme of ["dark", "light"]) {
  const { context, page } = await open(theme, DESKTOP);
  await window_(page, `01-desktop-${theme}`, DESKTOP);
  await element(page, ".planner-panel", `02-planner-${theme}`);
  await element(page, '[data-testid^="route-card-"]', `03-route-card-${theme}`);
  await element(page, ".results-panel, .route-results", `04-results-${theme}`);
  await context.close();
}

// 3. The map alone: the green route against the provider's traffic layer is
// the one picture that explains the product without a caption.
{
  const { context, page } = await open("dark", DESKTOP);
  const map = await page.locator(".map-stage").first().boundingBox();
  if (map) await page.screenshot({ path: `${OUT}/05-map-traffic-dark.png`, clip: map });
  console.log("✓ 05-map-traffic-dark");
  await page.getByRole("button", { name: "Увеличить масштаб" }).click();
  await page.waitForTimeout(2500);
  const zoomed = await page.locator(".map-stage").first().boundingBox();
  if (zoomed) await page.screenshot({ path: `${OUT}/06-map-detour-dark.png`, clip: zoomed });
  console.log("✓ 06-map-detour-dark");
  await context.close();
}

// 4. Saved work: favourites, recent searches and stored analyses.
{
  const { context, page } = await open("dark", DESKTOP);
  for (const [tab, name] of [["favorites", "07-favourites"], ["recent", "08-recent"], ["bookmarks", "09-bookmarks"]]) {
    const control = page.getByTestId(`preset-tab-${tab}`);
    if (await control.count() === 0) continue;
    await control.click();
    await page.waitForTimeout(900);
    await element(page, ".route-presets", name);
  }
  await context.close();
}

// 6. English, because the interface is not translated at the edges only.
{
  const { context, page } = await open("dark", DESKTOP, { locale: "en" });
  await window_(page, "11-desktop-english", DESKTOP);
  await context.close();
}

// 7. The same product on a tablet and on a phone.
{
  const tablet = { width: 1100, height: 1100 };
  let { context, page } = await open("light", tablet);
  await window_(page, "12-tablet-light", tablet);
  await context.close();

  const phone = { width: 430, height: 932 };
  for (const theme of ["dark", "light"]) {
    ({ context, page } = await open(theme, phone, { scale: 3 }));
    await window_(page, `13-mobile-${theme}`, phone);
    await context.close();
  }
}

await browser.close();
console.log(`\nscreenshots in ${OUT}`);
