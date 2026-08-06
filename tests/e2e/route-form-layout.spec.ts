import { expect, test } from "@playwright/test";

test("swap sits inside the destination field without covering its text", async ({ page }) => {
  await page.goto("/");
  await expect(page.locator('html[data-greenroute-hydrated="true"]')).toBeAttached();

  const inputs = page.locator(".location-input");
  const swap = page.locator(".swap-button");
  await expect(inputs).toHaveCount(2);
  await expect(swap).toBeVisible();

  const [destination, swapBox] = await Promise.all([inputs.last().boundingBox(), swap.boundingBox()]);
  expect(destination).not.toBeNull();
  expect(swapBox).not.toBeNull();
  if (!destination || !swapBox) return;

  // It lives in the field's trailing slot, so it must be the element under its
  // own centre — otherwise the input would swallow the tap.
  const centre = { x: swapBox.x + swapBox.width / 2, y: swapBox.y + swapBox.height / 2 };
  const hit = await page.evaluate(
    ({ x, y }) => document.elementFromPoint(x, y)?.closest(".swap-button") !== null,
    centre,
  );
  expect(hit).toBe(true);

  // And the typed address must stop before it rather than run underneath.
  const paddingRight = await inputs.last().evaluate((node) => Number.parseFloat(getComputedStyle(node).paddingRight));
  expect(paddingRight).toBeGreaterThanOrEqual(swapBox.width);
  expect(swapBox.x + swapBox.width).toBeLessThanOrEqual(destination.x + destination.width);
});

test("a waypoint keeps its remove control clear of the address it removes", async ({ page }) => {
  await page.goto("/");
  await expect(page.locator('html[data-greenroute-hydrated="true"]')).toBeAttached();

  await page.locator(".route-form > .text-button").click();
  const waypointInput = page.locator(".waypoint-row .location-input");
  const removeWaypoint = page.locator(".remove-waypoint");
  await expect(waypointInput).toBeVisible();
  await expect(removeWaypoint).toBeVisible();

  const [inputBox, removeBox] = await Promise.all([waypointInput.boundingBox(), removeWaypoint.boundingBox()]);
  expect(inputBox).not.toBeNull();
  expect(removeBox).not.toBeNull();
  if (!inputBox || !removeBox) return;
  const overlapWidth = Math.max(0, Math.min(inputBox.x + inputBox.width, removeBox.x + removeBox.width) - Math.max(inputBox.x, removeBox.x));
  expect(overlapWidth).toBe(0);
});
