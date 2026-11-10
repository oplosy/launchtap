import AxeBuilder from "@axe-core/playwright";
import { expect, test } from "@playwright/test";

const routes = [
  "/",
  "/explore",
  "/forum",
  "/create",
  "/analytics",
  "/docs",
  "/profile",
  "/notifications",
];

for (const theme of ["light", "dark"] as const) {
  test(`shell routes have no serious or critical accessibility violations in the ${theme} theme`, async ({
    page,
    context,
    baseURL,
  }) => {
    await context.addCookies([{ name: "launchpad-theme", value: theme, url: baseURL! }]);
    for (const route of routes) {
      const response = await page.goto(route);
      expect(response?.status(), `${route} should load successfully`).toBe(200);
      const results = await new AxeBuilder({ page }).analyze();
      const seriousOrCritical = results.violations.filter((violation) =>
        ["serious", "critical"].includes(violation.impact ?? ""),
      );
      await expect(page.locator("html")).toHaveAttribute("data-theme", theme);
      expect(seriousOrCritical, `${route} accessibility violations (${theme})`).toEqual([]);
    }
  });
}

test("analytics unavailable state is explicit when no reviewed API is configured", async ({
  page,
}) => {
  await page.goto("/analytics");
  await expect(page.getByRole("heading", { name: "Analytics unavailable" })).toBeVisible();
  await expect(page.getByText("No reviewed API deployment is connected.")).toBeVisible();
});

test("a visitor sees no notification bell", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByRole("button", { name: /^Notifications/ })).toHaveCount(0);
  await expect(page.locator(".notify-bell")).toHaveCount(0);
});
