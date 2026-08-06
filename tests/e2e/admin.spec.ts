import { expect, test } from "@playwright/test";

test("admin view lists provider contracts without inventing telemetry", async ({ page }) => {
  await page.goto("/admin");
  await expect(page.getByRole("heading", { name: "Операционный обзор" })).toBeVisible();

  // The page exists to answer "which provider API does what, and what does it
  // cost", one group per provider.
  await expect(page.getByRole("heading", { name: "2ГИС" })).toBeVisible();
  await expect(page.getByRole("heading", { name: "ЯНДЕКС" })).toBeVisible();
  await expect(page.getByText("ROUTING API").first()).toBeVisible();
  await expect(page.getByText("JAVASCRIPT API V3")).toBeVisible();

  // Modules the app does not call are labelled as such rather than dressed up
  // as healthy, and demo mode says outright that no telemetry is connected.
  await expect(page.getByText("Не используется").first()).toBeVisible();
  await expect(page.getByRole("heading", { name: "Реальная телеметрия не подключена" })).toBeVisible();
  await expect(page.getByText(/HEALTHY|UP · |\d+ ₽|18\s?426/)).toHaveCount(0);
});
