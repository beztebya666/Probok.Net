import { expect, test, type Page } from "@playwright/test";

async function selectEndpoints(page: Page) {
  await page.getByRole("combobox", { name: "Откуда", exact: true }).fill("Кремль");
  await page.getByRole("option", { name: /Московский Кремль/ }).click();
  await page.getByRole("combobox", { name: "Куда", exact: true }).fill("ВДНХ");
  await page.getByRole("option", { name: /ВДНХ/ }).click();
}

test("finds and compares route alternatives", async ({ page }) => {
  await page.goto("/");
  await expect(page.locator('html[data-greenroute-hydrated="true"]')).toBeAttached();
  await expect(page.getByRole("heading", { name: "Куда едем?" })).toBeVisible();
  await selectEndpoints(page);

  await page.getByText("Баланс", { exact: true }).click();
  // The allowances are folded away until asked for, so the planner stays short.
  await page.locator(".advanced-settings > summary").click();
  await page.getByRole("slider").first().fill("40");
  await page.getByRole("button", { name: "Найти маршрут" }).click();

  await expect(page.getByText("Анализируем варианты")).toBeVisible();
  await expect(page.getByRole("heading", { name: "Варианты маршрута" })).toBeVisible();
  await expect(page.getByTestId("route-card-route-green").getByText("Рекомендуем")).toBeVisible({ timeout: 10_000 });

  const fastest = page.getByTestId("route-card-route-fastest");
  await fastest.getByRole("button", { name: "Выбрать" }).click();
  await expect(fastest.getByRole("button", { name: /Выбран/ })).toBeDisabled();
});

test("cancels an enhanced search", async ({ page }) => {
  await page.goto("/");
  await expect(page.locator('html[data-greenroute-hydrated="true"]')).toBeAttached();
  await selectEndpoints(page);
  await page.getByRole("button", { name: "Найти маршрут" }).click();
  await page.getByRole("button", { name: "Отменить поиск" }).click();
  await expect(page.getByText("Изменить параметры")).toBeVisible();
});
