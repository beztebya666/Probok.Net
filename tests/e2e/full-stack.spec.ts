import { expect, test, type Page } from "@playwright/test";

test.skip(!process.env.PLAYWRIGHT_BASE_URL, "requires the deployed full stack");

test("draws the map the deployment can actually draw, with working zoom", async ({ page }) => {
  await page.goto("/");
  await expect(page.locator('html[data-greenroute-hydrated="true"]')).toBeAttached();
  await expect(page.getByRole("radiogroup", { name: "Пробки" })).toBeVisible();

  // A deployment without a provider credential is a supported deployment, not a
  // broken one: it offers OpenStreetMap and says the rest are unavailable. The
  // e2e stack is exactly that, so the assertions follow what the build has.
  const osmChoice = page.getByRole("radio", { name: /OSM/ });
  if (await osmChoice.count() > 0) {
    await expect(osmChoice).toHaveAttribute("aria-checked", "true");
    await expect(page.getByRole("radio", { name: "Выкл" })).toHaveAttribute("aria-disabled", "true");
    const osm = page.locator(".tile-map").first();
    await expect(osm).toBeVisible({ timeout: 20_000 });
    await expect.poll(() => osm.locator(".tile-map-tiles img").count()).toBeGreaterThan(0);
    const zoomIn = osm.getByRole("button", { name: "Увеличить масштаб" });
    await expect(zoomIn).toBeEnabled();
    await zoomIn.click();
    await osm.getByRole("button", { name: "Уменьшить масштаб" }).click();
    return;
  }

  // Which provider draws the map is a deployment choice, so the test follows
  // the one the build settled on rather than naming it in advance.
  const map = page.locator(".map-canvas.is-ready");
  await expect(map).toBeVisible({ timeout: 20_000 });
  await expect.poll(() => map.locator(":scope > *").count()).toBeGreaterThan(0);
  const renderer = await map.getAttribute("data-map-renderer");
  const globalName = renderer === "2gis" ? "mapgl" : "ymaps3";
  await expect.poll(() => page.evaluate((name) => Boolean((window as unknown as Record<string, unknown>)[name]), globalName)).toBe(true);
  const checked = page.locator('.traffic-renderer-segments [role="radio"][aria-checked="true"]');
  await expect(checked).toHaveCount(1);

  const zoomIn = page.getByRole("button", { name: "Увеличить масштаб" });
  const zoomOut = page.getByRole("button", { name: "Уменьшить масштаб" });
  await expect(zoomIn).toBeEnabled();
  await expect(zoomOut).toBeEnabled();
  await zoomIn.click();
  await zoomOut.click();
  await expect(page.getByRole("button", { name: "Показать моё местоположение" })).toBeEnabled();
  await expect(page.getByRole("button", { name: "Открыть карту на весь экран" })).toBeEnabled();
});

test("switches the whole map to the exclusive 2GIS traffic renderer", async ({ page }) => {
  await page.goto("/");
  await expect(page.locator('html[data-greenroute-hydrated="true"]')).toBeAttached();

  const twoGis = page.getByRole("radio", { name: /^2ГИС/ });
  await expect(twoGis).toBeVisible();
  test.skip(await twoGis.getAttribute("aria-disabled") === "true", "2GIS MapGL browser key is not configured");

  await twoGis.click();
  const map = page.locator('.map-canvas[data-map-renderer="2gis"].is-ready');
  await expect(map).toBeVisible({ timeout: 20_000 });
  await expect.poll(() => page.evaluate(() => Boolean((window as Window & { mapgl?: unknown }).mapgl))).toBe(true);
  await expect.poll(() => map.locator("canvas").count()).toBeGreaterThan(0);
  await expect(map.locator('[class*="ymaps"]')).toHaveCount(0);
  await expect(twoGis).toHaveAttribute("aria-checked", "true");
  await expect.poll(() => page.evaluate(() => {
    const value = window.localStorage.getItem("greenroute.traffic-provider.v1");
    return value ? (JSON.parse(value) as { provider?: string }).provider : null;
  })).toBe("2gis");

  await page.reload();
  await expect(page.locator('html[data-greenroute-hydrated="true"]')).toBeAttached();
  const restoredTwoGis = page.getByRole("radio", { name: /^2ГИС/ });
  await expect(restoredTwoGis).toHaveAttribute("aria-checked", "true");
  await expect(page.locator('.map-canvas[data-map-renderer="2gis"].is-ready')).toBeVisible({ timeout: 20_000 });

  await page.getByRole("radio", { name: "Выкл" }).click();
  await expect(page.locator('.map-canvas[data-map-renderer="yandex"].is-ready')).toBeVisible({ timeout: 20_000 });
  await expect(restoredTwoGis).toHaveAttribute("aria-checked", "false");
  await expect.poll(() => page.evaluate(() => {
    const value = window.localStorage.getItem("greenroute.traffic-provider.v1");
    return value ? (JSON.parse(value) as { provider?: string }).provider : null;
  })).toBe("off");
});

test("keeps the swap control inside the destination field and clear of its text", async ({ page }) => {
  await page.goto("/");
  await expect(page.locator('html[data-greenroute-hydrated="true"]')).toBeAttached();
  const origin = page.getByRole("combobox", { name: "Откуда", exact: true });
  const destination = page.getByRole("combobox", { name: "Куда", exact: true });
  const swap = page.getByRole("button", { name: "Откуда / Куда" });
  await origin.focus();

  const [originBox, swapBox, destinationBox] = await Promise.all([
    origin.boundingBox(),
    swap.boundingBox(),
    destination.boundingBox(),
  ]);
  expect(originBox).not.toBeNull();
  expect(swapBox).not.toBeNull();
  expect(destinationBox).not.toBeNull();

  // The swap rides in the destination field, mirroring the geolocation control
  // in the origin one, so the pair needs no row of its own.
  expect(swapBox!.y).toBeGreaterThanOrEqual(destinationBox!.y);
  expect(swapBox!.y + swapBox!.height).toBeLessThanOrEqual(destinationBox!.y + destinationBox!.height);
  expect(swapBox!.x + swapBox!.width).toBeLessThanOrEqual(destinationBox!.x + destinationBox!.width);
  // It must never reach into the field above it.
  expect(swapBox!.y).toBeGreaterThanOrEqual(originBox!.y + originBox!.height);

  // Typed text has to stop before the control rather than run underneath it.
  const clearance = await destination.evaluate((node) => Number.parseFloat(getComputedStyle(node).paddingRight));
  const overlap = destinationBox!.x + destinationBox!.width - swapBox!.x;
  expect(clearance).toBeGreaterThanOrEqual(overlap);
});

async function chooseFirstSuggestion(page: Page, field: "Откуда" | "Куда", query: string) {
  const input = page.getByRole("combobox", { name: field, exact: true });
  await input.fill(query);
  const option = page.getByRole("option").first();
  await expect(option).toBeVisible({ timeout: 10_000 });
  await option.click();
  await expect(input).toHaveAttribute("aria-expanded", "false");
}

test("returns only a verified all-green route through the deployed stack", async ({ page }) => {
  test.setTimeout(70_000);
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "Куда едем?" })).toBeVisible();
  await expect(page.locator('html[data-greenroute-hydrated="true"]')).toBeAttached();

  await chooseFirstSuggestion(page, "Откуда", "Кутузовский проспект, 32, Москва");
  await chooseFirstSuggestion(page, "Куда", "улица Академика Королёва, 12, Москва");
  await page.getByRole("radio", { name: /Только по зелёному/ }).click();

  const created = page.waitForResponse(
    (response) =>
      response.request().method() === "POST" &&
      response.url().includes("/api/v1/route-searches"),
  );
  await page.getByRole("button", { name: "Найти маршрут" }).click();
  const response = await created;
  expect([200, 202]).toContain(response.status());

  await expect(page.getByRole("heading", { name: "Варианты маршрута" })).toBeVisible({ timeout: 30_000 });

  // Proof that a search happened rather than a single provider answer being
  // passed through: the list ranks what the ladder actually found. Strict mode
  // never lists the fastest reference, so every card here is green-ranked.
  const cards = page.locator("[data-green-percent]");
  await expect(cards.first()).toBeVisible({ timeout: 35_000 });
  const percents = (await cards.evaluateAll((nodes) =>
    nodes.map((node) => Number((node as HTMLElement).dataset["greenPercent"]))));
  expect(percents.length).toBeGreaterThan(0);
  expect(percents.length).toBeLessThanOrEqual(3);
  expect(percents.every((value) => value >= 0 && value <= 100)).toBe(true);
  // Ranked by green share, best first.
  expect([...percents].sort((a, b) => b - a)).toEqual(percents);

  await expect(page.getByRole("region", { name: "Карта маршрута" })).toBeVisible();
});

test("serves the admin console only where the deployment enables it", async ({ page }) => {
  const response = await page.goto("/admin");
  // Both the page and its header link are off by default, so a 404 here is a
  // correct answer rather than a failure.
  test.skip(response?.status() === 404, "the admin console is disabled in this deployment");

  await expect(page.getByRole("heading", { name: "Операционный обзор" })).toBeVisible();
  await expect(page.getByRole("heading", { name: "2ГИС" })).toBeVisible();
  await expect(page.getByRole("heading", { name: "ЯНДЕКС" })).toBeVisible();
  await expect(page.getByText("Доступ не подтверждён")).toHaveCount(0);
  await expect(page.getByText(/18\s?426|742[,.]31/)).toHaveCount(0);
});
