/**
 * Freezes one real search so the demo can show the product, not a mock-up.
 *
 * Runs the live stack through the browser exactly as a user would, waits for a
 * terminal result, and writes the analysis with its request into the file the
 * demo build ships. Nothing is synthesised: the geometry, the segment colours
 * and the percentages are what the provider returned at the moment named in the
 * file.
 *
 *   node tools/docs-media/freeze-live-search.mjs "<from>" "<to>" [mode]
 *
 * A green search spends up to eight objects of the 2GIS daily allowance, so run
 * it deliberately.
 */
import { chromium } from "playwright-core";
import { writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const BASE = process.env.FREEZE_BASE_URL ?? "http://localhost:3000";
const [from, to, mode = "Свободнее"] = process.argv.slice(2);
if (!from || !to) throw new Error('usage: freeze-live-search.mjs "<from>" "<to>" [mode]');

const here = dirname(fileURLToPath(import.meta.url));
const browser = await chromium.launch({ channel: "chromium" });
const context = await browser.newContext({ viewport: { width: 1440, height: 900 }, locale: "ru-RU", timezoneId: "Europe/Moscow" });
await context.addInitScript(() => {
  window.localStorage.setItem("greenroute.traffic-provider.v1", JSON.stringify({ version: 1, provider: "2gis" }));
});
const page = await context.newPage();
// The cached result does not always carry the request that produced it, and the
// demo needs one to offer a refresh, so it is taken from the wire.
let submitted;
// The finished analysis is taken off the wire rather than out of the page's
// cache: a wide allowance produces geometry past the storage budget, and the
// page then keeps nothing at all.
let completed;
page.on("request", (request) => {
  if (request.method() === "POST" && request.url().includes("/api/v1/route-searches")) {
    try { submitted = JSON.parse(request.postData() ?? "null"); } catch { submitted = undefined; }
  }
});
page.on("response", async (response) => {
  if (!response.url().includes("/api/v1/route-searches")) return;
  let body;
  try { body = await response.text(); } catch { return; }
  // Two shapes carry the same answer: the plain GET, and the completion event
  // on the stream the page listens to.
  for (const candidate of [body, ...body.split(/^data: /m).slice(1)]) {
    let parsed;
    try { parsed = JSON.parse(candidate); } catch { continue; }
    const result = parsed?.result ?? parsed;
    if (result?.status === "COMPLETED" && Array.isArray(result.greenTopRoutes)) completed = result;
  }
});
await page.goto(BASE, { waitUntil: "domcontentloaded" });
await page.locator('html[data-greenroute-hydrated="true"]').waitFor();
await page.waitForTimeout(2500);

async function pick(field, query) {
  const input = page.getByRole("combobox", { name: field, exact: true });
  await input.click();
  await input.fill(query);
  const option = page.getByRole("option").first();
  await option.waitFor({ timeout: 20_000 });
  await page.waitForTimeout(500);
  const label = (await option.innerText()).split("\n")[0].trim();
  await option.click();
  return label;
}

const originLabel = await pick("Откуда", from);
const destinationLabel = await pick("Куда", to);
await page.getByText(mode, { exact: true }).click();
// The widest allowance the UI offers: the point is to see the detour it finds.
await page.locator(".advanced-settings > summary").click();
const sliders = page.getByRole("slider");
await sliders.nth(0).fill(process.env.FREEZE_EXTRA_KM ?? "150");
await sliders.nth(1).fill(process.env.FREEZE_EXTRA_MIN ?? "300");
await page.locator(".advanced-settings > summary").click();

await page.getByRole("button", { name: "Найти маршрут" }).click();
await page.getByRole("heading", { name: "Варианты маршрута" }).waitFor({ timeout: 120_000 });
await page.waitForTimeout(6000);

// The page caches the finished analysis, but only while it fits the storage
// budget, and a wide allowance produces geometry that does not. The API holds
// the same answer whole, so it is the fallback rather than the exception.
const stored = await page.evaluate(() => window.localStorage.getItem("greenroute.last-result.v1"));
const cached = stored ? JSON.parse(stored) : {};
cached.result ??= completed;
if (!cached.result) throw new Error("the search produced no result to freeze");
// Printed before the check below: a search that comes back empty is exactly
// when its warnings and provider usage are worth reading.
console.log(JSON.stringify({
  // The two points the search actually asked about: a suggestion list can hand
  // back a namesake in another city, and the provider then refuses a distance
  // nobody meant to request.
  origin: submitted?.origin,
  destination: submitted?.destination,
  status: cached.result.status,
  ranked: (cached.result.greenTopRoutes ?? []).length,
  alternatives: (cached.result.alternatives ?? []).length,
  providerUsage: cached.result.providerUsage,
  warnings: (cached.result.warnings ?? []).map((warning) => warning?.code ?? warning),
}));
if (!cached.result.greenTopRoutes?.length) throw new Error("the search returned no ranked routes");

// The wire body has no requestId — the edge API assigns one — while the client
// schema requires it, so the result's own id fills the gap.
const captured = cached.request ?? submitted;
if (!captured) throw new Error("could not capture the request that produced this result");
const request = captured.requestId ? captured : { ...captured, requestId: cached.result.requestId };
if (!request.requestId) throw new Error("the captured request has no requestId");
const payload = {
  result: cached.result,
  request,
  labels: { origin: originLabel, destination: destinationLabel },
  capturedAt: cached.result.generatedAt,
};
// Written relative to the repository root rather than to this file, so running
// a copy of the script from anywhere still updates the fixture that ships.
const repoRoot = process.env.PROBOK_ROOT ?? join(here, "..", "..");
const payloadText = JSON.stringify(payload);
writeFileSync(join(repoRoot, "apps", "web", "public", "demo", "analysis.json"), payloadText, "utf8");
writeFileSync(join(repoRoot, "tools", "docs-media", "demo-analysis.json"), payloadText, "utf8");

const summary = cached.result.greenTopRoutes.map((route) => {
  const total = route.liveDurationSeconds;
  const green = route.metrics?.greenDurationSeconds ?? 0;
  const red = route.metrics?.redDurationSeconds ?? 0;
  const orange = route.metrics?.orangeDurationSeconds ?? 0;
  return {
    id: route.candidateId,
    greenPercent: Math.round((green / total) * 100),
    minutes: Math.round(total / 60),
    km: +(route.distanceMeters / 1000).toFixed(1),
    redMinutes: Math.round(red / 60),
    orangeMinutes: Math.round(orange / 60),
  };
});
console.log(JSON.stringify({ origin: originLabel, destination: destinationLabel, capturedAt: payload.capturedAt, routes: summary }, null, 2));
await browser.close();
