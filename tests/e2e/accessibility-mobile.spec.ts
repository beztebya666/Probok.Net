import { expect, test } from "@playwright/test";

test("mobile UI exposes confidence and UNKNOWN coverage without colour-only meaning", async ({ page }) => {
  await page.goto("/");
  await expect(page.locator('html[data-greenroute-hydrated="true"]')).toBeAttached();
  await page.getByRole("combobox", { name: "Откуда", exact: true }).fill("Кремль");
  await page.getByRole("option", { name: /Московский Кремль/ }).click();
  await page.getByRole("combobox", { name: "Куда", exact: true }).fill("ВДНХ");
  await page.getByRole("option", { name: /ВДНХ/ }).click();
  await page.getByRole("button", { name: "Найти маршрут" }).click();

  await expect(page.getByText(/Для 25% пути данных недостаточно/)).toBeVisible({ timeout: 10_000 });
  await expect(page.getByText(/Средняя · 66%/)).toBeVisible();
  await expect(page.getByText("Схематичная карта").first()).toBeVisible();
});

test("keyboard users can choose geosuggest results", async ({ page }) => {
  await page.goto("/");
  await expect(page.locator('html[data-greenroute-hydrated="true"]')).toBeAttached();
  const origin = page.getByRole("combobox", { name: "Откуда", exact: true });
  await origin.fill("Кремль");
  await expect(page.getByRole("option", { name: /Московский Кремль/ })).toBeVisible();
  await origin.press("ArrowDown");
  await origin.press("Enter");
  await expect(origin).toHaveValue("Московский Кремль");
});
