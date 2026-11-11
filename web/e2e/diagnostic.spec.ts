import { expect, test } from "@playwright/test";

test("application shell reports fail-closed configuration", async ({ page }) => {
  await page.goto("/explore");
  await expect(page.getByRole("heading", { level: 1, name: "Explore" })).toBeVisible();
  await expect(page.getByText("Token discovery unavailable").first()).toBeVisible();
  if ((page.viewportSize()?.width ?? 0) <= 720)
    await expect(page.getByRole("button", { name: "Open menu" })).toBeVisible();
  else await expect(page.getByRole("navigation", { name: "Primary" })).toBeVisible();
});

test("mobile shell keeps primary routes keyboard reachable", async ({ page }) => {
  await page.setViewportSize({ width: 360, height: 800 });
  await page.goto("/");
  await page.getByRole("button", { name: "Open menu" }).focus();
  await expect(page.getByRole("button", { name: "Open menu" })).toBeFocused();
  await page.getByRole("button", { name: "Open menu" }).click();
  const drawer = page.getByRole("dialog", { name: "Menu" });
  await expect(drawer).toBeVisible();
  for (const label of ["Explore", "Forum", "Create", "Analytics", "Docs", "Profile"]) {
    await expect(drawer.getByRole("link", { name: label })).toBeVisible();
  }
});

test("explore view tabs restore through browser history", async ({ page }) => {
  await page.goto("/explore");
  const tabs = page.getByRole("navigation", { name: "Explore views" });
  await tabs.getByRole("link", { name: "Graduated" }).click();
  await expect(page).toHaveURL(/\/explore\?view=graduated$/);
  await page.goBack();
  await expect(page).toHaveURL(/\/explore$/);
  await expect(tabs.getByRole("link", { name: "Trending" })).toHaveAttribute(
    "aria-current",
    "page",
  );
  await page.goForward();
  await expect(page).toHaveURL(/\/explore\?view=graduated$/);
  await expect(tabs.getByRole("link", { name: "Graduated" })).toHaveAttribute(
    "aria-current",
    "page",
  );
});
