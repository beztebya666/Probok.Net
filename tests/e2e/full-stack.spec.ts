import { expect, test, type Page } from "@playwright/test";

test.skip(!process.env.PLAYWRIGHT_BASE_URL, "requires the deployed full stack");

test("loads the interactive Yandex JavaScript API v3 map and accessible controls", async ({ page }) => {
  await page.goto("/");
  await expect(page.locator('html[data-greenroute-hydrated="true"]')).toBeAttached();
  const map = page.locator('.map-canvas[data-map-renderer="yandex"].is-ready');
  await expect(map).toBeVisible({ timeout: 20_000 });
  await expect.poll(() => map.locator(":scope > *").count()).toBeGreaterThan(0);
  await expect.poll(() => page.evaluate(() => Boolean((window as Window & { ymaps3?: unknown }).ymaps3))).toBe(true);

  await expect(page.getByRole("radiogroup", { name: "Пробки" })).toBeVisible();
  await expect(page.getByRole("radio", { name: "Выкл" })).toHaveAttribute("aria-checked", "true");
  const paidYandexTraffic = page.getByRole("radio", { name: /Яндекс/ });
  await expect(paidYandexTraffic).toHaveAttribute("aria-disabled", "true");
  await expect(paidYandexTraffic).toHaveAttribute("title", "Нужен платный тариф");

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

test("keeps the swap control outside both address focus outlines", async ({ page }) => {
  await page.goto("/");
  await expect(page.locator('html[data-greenroute-hydrated="true"]')).toBeAttached();
  const origin = page.getByRole("combobox", { name: "Откуда" });
  const destination = page.getByRole("combobox", { name: "Куда" });
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
  expect(originBox!.y + originBox!.height).toBeLessThanOrEqual(swapBox!.y);
  expect(swapBox!.y + swapBox!.height).toBeLessThanOrEqual(destinationBox!.y);
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
  // passed through: the podium ranks what the ladder actually found.
  const podium = page.locator(".green-podium");
  await expect(podium).toBeVisible({ timeout: 35_000 });
  const ranked = podium.locator(".green-podium-row");
  await expect(ranked.first()).toBeVisible();
  const shares = await ranked.locator(".green-podium-share").allInnerTexts();
  expect(shares.length).toBeGreaterThan(0);
  expect(shares.length).toBeLessThanOrEqual(3);
  const percents = shares.map((text) => Number.parseInt(text.replace(/\D+/g, ""), 10));
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
