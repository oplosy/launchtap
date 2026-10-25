import { expect, test } from "@playwright/test";

test("application shell reports fail-closed configuration", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "Explore launches." })).toBeVisible();
  await expect(page.getByText("Token discovery unavailable").first()).toBeVisible();
  if ((page.viewportSize()?.width ?? 0) <= 720)
    await expect(page.getByRole("button", { name: "Open navigation" })).toBeVisible();
  else
    await expect(
      page.getByRole("navigation", { name: "Desktop primary navigation" }),
    ).toBeVisible();
});

test("mobile shell keeps primary routes keyboard reachable", async ({ page }) => {
  await page.setViewportSize({ width: 360, height: 800 });
  await page.goto("/");
  await page.getByRole("button", { name: "Open navigation" }).focus();
  await expect(page.getByRole("button", { name: "Open navigation" })).toBeFocused();
  await page.getByRole("button", { name: "Open navigation" }).click();
  const drawer = page.getByRole("dialog", { name: "Navigate" });
  await expect(drawer).toBeVisible();
  for (const label of ["Explore", "Create", "Analytics", "Docs", "Profile"]) {
    await expect(drawer.getByRole("link", { name: label })).toBeVisible();
  }
});

test("legacy graduated route redirects to the consolidated explore view", async ({ page }) => {
  await page.goto("/graduated?phase=curve&sort=market_cap");
  await expect(page.getByRole("heading", { name: "Explore launches." })).toBeVisible();
  await expect(page).toHaveURL(/\/$/);
});

test("discovery filters restore through browser history", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("button", { name: "Volume", exact: true }).click();
  await expect(page).toHaveURL(/\/\?sort=volume_24h$/);
  await page.goBack();
  await expect(page).toHaveURL(/127\.0\.0\.1:\d+\/$/);
  await expect(page.getByRole("button", { name: "Newest", exact: true })).toHaveAttribute(
    "aria-pressed",
    "true",
  );
  await page.goForward();
  await expect(page).toHaveURL(/\/\?sort=volume_24h$/);
  await expect(page.getByRole("button", { name: "Volume", exact: true })).toHaveAttribute(
    "aria-pressed",
    "true",
  );
});
