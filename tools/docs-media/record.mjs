/**
 * Records the animated clips used by the README.
 *
 * Runs against a demo-mode server, so the search below is the real interaction
 * against demo fixtures rather than a staged mock-up. Frames are grabbed on a
 * timer while the script drives the page, then assembled by build-clips.py.
 *
 *   node tools/docs-media/record.mjs http://127.0.0.1:3200 docs/media/frames
 */
import { createRequire } from "node:module";
import { mkdirSync, rmSync, writeFileSync, readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join, resolve } from "node:path";

// The browser lives in the web app's dependencies, and ESM resolves imports
// from this file's directory, so it is asked for by path rather than by name.
const { chromium } = createRequire(new URL("../../apps/web/package.json", import.meta.url))("playwright-core");

const BASE = process.argv[2] ?? "http://localhost:3000";
const ROOT = resolve(process.argv[3] ?? "docs/media/frames");
// Recording against the live stack shows the real map with its traffic layer;
// the frozen analysis is what a saved search restores there too.
const LIVE = !process.argv.includes("--demo");
const here = dirname(fileURLToPath(import.meta.url));
const fixture = JSON.parse(readFileSync(process.env.PACK_FIXTURE ?? join(here, "demo-analysis.json"), "utf8"));

const SAVED = JSON.stringify({
  version: 1,
  savedAt: Date.parse(fixture.result.generatedAt),
  routingMode: fixture.request.routingMode,
  selectedCandidateId: (fixture.result.selectedRoute ?? fixture.result.greenTopRoutes?.[0])?.candidateId,
  request: fixture.request,
  result: fixture.result,
});

const browser = await chromium.launch({ channel: "chromium" });

// Recording one clip at a time matters when a clip costs provider requests.
const ONLY = process.env.RECORD_ONLY?.split(",").map((entry) => entry.trim()).filter(Boolean);

async function record(name, { width, height, theme, restore = false, fps = 3 }, script) {
  if (ONLY && !ONLY.includes(name)) return;
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
    ([theme, saved, restore, live]) => {
      window.localStorage.setItem("greenroute.theme.v1", theme);
      window.localStorage.setItem("greenroute.traffic-provider.v1", JSON.stringify({ version: 1, provider: "2gis" }));
      if (restore || live) window.localStorage.setItem("greenroute.last-result.v1", saved);
    },
    [theme, SAVED, restore, LIVE],
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
  if (LIVE && !process.env.RECORD_FULL_SEARCH) {
    // Address lookup is a geocoder call, not a routing one, so this much of the
    // flow can be recorded without spending the routing allowance. The analysis
    // already on screen is a real one, restored rather than re-run.
    const type = async (field, query) => {
      const input = page.getByRole("combobox", { name: field, exact: true });
      await input.click();
      await input.fill("");
      await input.pressSequentially(query, { delay: 110 });
      await page.getByRole("option").first().waitFor({ timeout: 20_000 });
      await page.waitForTimeout(1100);
      await page.getByRole("option").first().click();
      await page.waitForTimeout(700);
    };
    await type("Откуда", process.env.RECORD_FROM ?? "Коломенское");
    await type("Куда", process.env.RECORD_TO ?? "Одинцово, Можайское шоссе");
    await page.getByText("Свободнее", { exact: true }).click();
    await page.waitForTimeout(2200);
    return;
  }
  if (LIVE) {
    // The whole answer, live: RECORD_FULL_SEARCH spends a real routing
    // allowance, which is why it is not the default.
    const pick = async (field, query) => {
      const input = page.getByRole("combobox", { name: field, exact: true });
      await input.click();
      await input.fill("");
      await input.pressSequentially(query, { delay: 90 });
      const option = page.getByRole("option").first();
      await option.waitFor({ timeout: 20_000 });
      await page.waitForTimeout(900);
      await option.click();
      await page.waitForTimeout(500);
    };
    await pick("Откуда", process.env.RECORD_FROM ?? "Коломенское");
    await pick("Куда", process.env.RECORD_TO ?? "Одинцово, Можайское шоссе");
    await page.getByText("Свободнее", { exact: true }).click();
    await page.waitForTimeout(800);
    await page.getByRole("button", { name: "Найти маршрут" }).click();
    await page.getByRole("heading", { name: "Варианты маршрута" }).waitFor({ timeout: 90_000 });
    await page.waitForTimeout(4500);
    return;
  }
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

// Choosing another of the ranked routes redraws the map — the proof that the
// ranking is real routes and not three numbers.
await record("podium", { width: 1280, height: 800, theme: "light", restore: true, fps: 3 }, async (page) => {
  const choose = page.getByRole("button", { name: "Выбрать" });
  const count = Math.min(2, await choose.count());
  for (let index = 0; index < count; index++) {
    await page.getByRole("button", { name: "Выбрать" }).first().click();
    await page.waitForTimeout(2600);
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
  await page.getByTestId("preset-tab-bookmarks").click();
  await page.waitForTimeout(1600);
  const saved = page.locator(".route-bookmark-main").first();
  if (await saved.count()) {
    await saved.click();
    await page.waitForTimeout(2000);
  }
  // The same control both saves and forgets, so the clip shows both.
  await page.locator(".results-bookmark").click();
  await page.waitForTimeout(1400);
  await page.locator(".results-bookmark").click();
  await page.waitForTimeout(1800);
});

await browser.close();
