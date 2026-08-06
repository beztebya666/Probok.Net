/**
 * Records the animated clips used by the README.
 *
 * Runs against a demo-mode server, so the search below is the real interaction
 * against demo fixtures rather than a staged mock-up. Frames are grabbed on a
 * timer while the script drives the page, then assembled by build-clips.py.
 *
 *   node tools/docs-media/record.mjs http://127.0.0.1:3200 docs/media/frames
 */
import { chromium } from "playwright-core";
import { mkdirSync, rmSync, writeFileSync, readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join, resolve } from "node:path";

const BASE = process.argv[2] ?? "http://127.0.0.1:3200";
const ROOT = resolve(process.argv[3] ?? "docs/media/frames");
const here = dirname(fileURLToPath(import.meta.url));
const fixture = JSON.parse(readFileSync(join(here, "demo-analysis.json"), "utf8"));

const SAVED = JSON.stringify({
  version: 1,
  savedAt: Date.parse(fixture.result.generatedAt),
  routingMode: "GREENEST",
  selectedCandidateId: fixture.result.selectedRoute.candidateId,
  request: fixture.request,
  result: fixture.result,
});

const browser = await chromium.launch({ channel: "chromium" });

async function record(name, { width, height, theme, restore = false, fps = 3 }, script) {
  const dir = join(ROOT, name);
  rmSync(dir, { recursive: true, force: true });
  mkdirSync(dir, { recursive: true });

  const context = await browser.newContext({
    viewport: { width, height },
    deviceScaleFactor: 1,
    locale: "ru-RU",
    timezoneId: "Europe/Moscow",
    reducedMotion: "no-preference",
  });
  await context.addInitScript(
    ([theme, saved, restore]) => {
      window.localStorage.setItem("greenroute.theme.v1", theme);
      window.localStorage.setItem("greenroute.traffic-provider.v1", JSON.stringify({ version: 1, provider: "2gis" }));
      if (restore) window.localStorage.setItem("greenroute.last-result.v1", saved);
    },
    [theme, SAVED, restore],
  );
  const page = await context.newPage();
  await page.goto(BASE, { waitUntil: "domcontentloaded" });
  await page.locator('html[data-greenroute-hydrated="true"]').waitFor();
  await page.waitForTimeout(restore ? 6000 : 3500);

  let index = 0;
  let recording = true;
  const grab = (async () => {
    while (recording) {
      await page.screenshot({ path: join(dir, `${String(index++).padStart(3, "0")}.png`) });
      await page.waitForTimeout(Math.max(0, Math.round(1000 / fps) - 160));
    }
  })();

  await script(page);
  recording = false;
  await grab;
  writeFileSync(join(dir, "meta.json"), JSON.stringify({ fps, frames: index }), "utf8");
  console.log(`✓ ${name}: ${index} frames`);
  await context.close();
}

// The whole point of the app, end to end: two addresses, a mode, a search, and
// three routes ranked by how much of them is green.
await record("search", { width: 1280, height: 800, theme: "light", fps: 3 }, async (page) => {
  const origin = page.getByRole("combobox", { name: "Откуда", exact: true });
  await origin.click();
  await origin.pressSequentially("Кремль", { delay: 130 });
  await page.getByRole("option").first().waitFor();
  await page.waitForTimeout(900);
  await page.getByRole("option").first().click();

  const destination = page.getByRole("combobox", { name: "Куда", exact: true });
  await destination.click();
  await destination.pressSequentially("ВДНХ", { delay: 130 });
  await page.getByRole("option").first().waitFor();
  await page.waitForTimeout(900);
  await page.getByRole("option").first().click();
  await page.waitForTimeout(600);

  await page.getByText("Свободнее", { exact: true }).click();
  await page.waitForTimeout(700);
  await page.getByRole("button", { name: "Найти маршрут" }).click();
  await page.getByRole("heading", { name: "Варианты маршрута" }).waitFor({ timeout: 30_000 });
  await page.waitForTimeout(3500);
});

// Clicking a rank previews that route on the map — the proof that the ranking
// is three real routes and not three numbers.
await record("podium", { width: 1280, height: 800, theme: "light", restore: true, fps: 3 }, async (page) => {
  for (const rank of [2, 3, 1]) {
    await page.getByTestId(`green-rank-${rank}`).click();
    await page.waitForTimeout(2200);
  }
});

await record("theme", { width: 1280, height: 800, theme: "light", restore: true, fps: 4 }, async (page) => {
  const toggle = page.getByTestId("theme-toggle");
  await page.waitForTimeout(800);
  await toggle.click();
  await page.waitForTimeout(2200);
  await toggle.click();
  await page.waitForTimeout(1800);
});

// Saving an analysis and reopening it from the bookmarks tab, which costs no
// provider request at all.
await record("bookmark", { width: 1280, height: 800, theme: "dark", restore: true, fps: 3 }, async (page) => {
  await page.waitForTimeout(700);
  await page.locator(".results-bookmark").click();
  await page.waitForTimeout(1400);
  await page.getByTestId("preset-tab-bookmarks").click();
  await page.waitForTimeout(1800);
  await page.locator(".route-bookmark-main").first().click();
  await page.waitForTimeout(2200);
});

await browser.close();
