import { expect, test } from "@playwright/test";

test.describe("Task 6 transaction safety shell", () => {
  test("create route fails closed without reviewed deployment and never exposes a submit action", async ({
    page,
  }) => {
    await page.goto("/create");
    await expect(page.getByText("V1", { exact: true })).toBeVisible();
    await expect(page.getByRole("heading", { name: "Transactions unavailable" })).toBeVisible();
    await expect(page.getByText(/No transaction can be submitted/)).toBeVisible();
    await expect(page.getByRole("button", { name: /Sign|Review launch/i })).toHaveCount(0);
  });

  test("mobile create safety state has no horizontal overflow", async ({ page }) => {
    await page.setViewportSize({ width: 360, height: 800 });
    await page.goto("/create");
    await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth)).toBe(360);
    await expect(page.getByRole("heading", { name: "Transactions unavailable" })).toBeVisible();
    await expect(
      page.locator(".app-footer").getByText(/Launchpad never takes custody/),
    ).toBeVisible();
  });
});
