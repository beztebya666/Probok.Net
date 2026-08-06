/**
 * Renders the documentation screenshots.
 *
 *   node tools/docs-media/shoot.mjs <base-url> <out-dir> [--demo]
 *
 * Without --demo it shoots the live stack, so the map is the provider's with its
 * traffic layer and the analysis is whatever that build restores. With --demo it
 * shoots the published demo exactly as a visitor sees it.
 *
 * Every full-window shot is clipped to the last route card that fits: a card cut
 * in half by the bottom edge looks like a bug in the product rather than a crop.
 */
import { chromium } from "playwright-core";
import { readFileSync, mkdirSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join, resolve } from "node:path";

const BASE = process.argv[2] ?? "http://localhost:3000";
const OUT = resolve(process.argv[3] ?? "docs/media");
const DEMO = process.argv.includes("--demo");
const here = dirname(fileURLToPath(import.meta.url));
const fixture = JSON.parse(readFileSync(join(here, "demo-analysis.json"), "utf8"));
mkdirSync(OUT, { recursive: true });

const SAVED = JSON.stringify({
  version: 1,
  savedAt: Date.parse(fixture.result.generatedAt),
  routingMode: fixture.request.routingMode,
  selectedCandidateId: fixture.result.greenTopRoutes?.[0]?.candidateId,
  request: fixture.request,
  result: fixture.result,
});

const browser = await chromium.launch({ channel: "chromium" });

async function open(theme, size, scale = 2) {
  const context = await browser.newContext({
    viewport: size,
    deviceScaleFactor: scale,
    locale: "ru-RU",
    timezoneId: "Europe/Moscow",
    isMobile: size.width < 520,
    hasTouch: size.width < 520,
  });
  await context.addInitScript(([theme, saved, demo]) => {
    window.localStorage.setItem("greenroute.theme.v1", theme);
    if (!demo) {
      window.localStorage.setItem("greenroute.traffic-provider.v1", JSON.stringify({ version: 1, provider: "2gis" }));
      window.localStorage.setItem("greenroute.last-result.v1", saved);
    }
  }, [theme, SAVED, DEMO]);
  const page = await context.newPage();
  await page.goto(BASE, { waitUntil: "domcontentloaded" });
  await page.locator('html[data-greenroute-hydrated="true"]').waitFor({ timeout: 30_000 });
  if (!DEMO) {
    await page.getByRole("combobox", { name: "Откуда", exact: true }).fill(fixture.labels.origin);
    await page.keyboard.press("Escape");
    await page.getByRole("combobox", { name: "Куда", exact: true }).fill(fixture.labels.destination);
    await page.keyboard.press("Escape");
    await page.locator("body").click({ position: { x: 2, y: 2 } });
  }
  await page.waitForTimeout(9000);
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

async function window_(page, path, size) {
  const height = await cleanHeight(page, size.height);
  await page.screenshot({ path, clip: { x: 0, y: 0, width: size.width, height } });
}

for (const theme of ["light", "dark"]) {
  const desktop = { width: 1440, height: 900 };
  let { context, page } = await open(theme, desktop);
  await window_(page, `${OUT}/desktop-${theme}.png`, desktop);
  await page.locator('[data-testid^="route-card-"]').first().screenshot({ path: `${OUT}/podium-${theme}.png` });
  await page.locator(".planner-panel").screenshot({ path: `${OUT}/planner-${theme}.png` });
  await context.close();

  const phone = { width: 430, height: 932 };
  ({ context, page } = await open(theme, phone, 3));
  await window_(page, `${OUT}/mobile-${theme}.png`, phone);
  await context.close();
}

let { context, page } = await open("light", { width: 1440, height: 900 });
const map = await page.locator(".map-region, .route-map, .map-canvas").first().boundingBox();
if (map) await page.screenshot({ path: `${OUT}/detour.png`, clip: map });
await page.getByTestId("preset-tab-bookmarks").click();
await page.waitForTimeout(600);
await page.locator(".route-presets").screenshot({ path: `${OUT}/bookmarks-light.png` });
await context.close();

const tablet = { width: 1100, height: 1000 };
({ context, page } = await open("light", tablet));
await window_(page, `${OUT}/tablet-light.png`, tablet);
await context.close();

await browser.close();
console.log(`screenshots in ${OUT}${DEMO ? " (demo build)" : " (live stack)"}`);
