import { expect, test } from "@playwright/test";

const NAV = ["Explore", "Forum", "Leaderboard", "Analytics", "Docs"] as const;

test("desktop nav centres five links and the Explore mega menu is keyboard operable", async ({
  page,
}, testInfo) => {
  test.skip(testInfo.project.name === "mobile", "Desktop navigation only.");
  await page.goto("/forum");
  const primary = page.getByRole("navigation", { name: "Primary" });
  await expect(primary).toBeVisible();
  for (const label of NAV.slice(1)) {
    await expect(primary.getByRole("link", { name: label, exact: true })).toBeVisible();
  }
  await expect(
    page.getByRole("banner").getByRole("link", { name: "Create", exact: true }),
  ).toBeVisible();

  const explore = primary.getByRole("button", { name: "Explore", exact: true });
  await expect(explore).toHaveAttribute("aria-expanded", "false");
  await explore.focus();
  await page.keyboard.press("Enter");
  const menu = page.getByRole("region", { name: "Explore menu" });
  await expect(menu).toBeVisible();
  await expect(explore).toHaveAttribute("aria-expanded", "true");
  await expect(menu.getByRole("link", { name: "Graduated" })).toHaveAttribute(
    "href",
    "/explore?view=graduated",
  );
  await expect(menu.getByRole("link", { name: "Watchlist" })).toHaveAttribute(
    "href",
    "/explore?view=watchlist",
  );

  // Tab reaches the menu's links, Escape closes it and returns focus to the trigger.
  await page.keyboard.press("Tab");
  await expect(menu.getByRole("link").first()).toBeFocused();
  await page.keyboard.press("Escape");
  await expect(menu).toBeHidden();
  await expect(explore).toBeFocused();

  // Focus leaving the trigger and the menu closes it.
  await page.keyboard.press("Enter");
  await expect(menu).toBeVisible();
  await primary.getByRole("link", { name: "Forum", exact: true }).focus();
  await expect(menu).toBeHidden();

  // Pointer: hovering Explore opens it.
  await explore.hover();
  await expect(menu).toBeVisible();
});

test("mobile menu opens a sheet with the same links", async ({ page }) => {
  await page.setViewportSize({ width: 360, height: 800 });
  await page.goto("/forum");
  // The wallet button keeps its place on mobile; its name depends on the build's wallet config.
  await expect(page.getByRole("banner").getByRole("button", { name: /wallet/i })).toBeVisible();
  const open = page.getByRole("button", { name: "Open menu" });
  await open.click();
  const sheet = page.getByRole("dialog", { name: "Menu" });
  await expect(sheet).toBeVisible();
  for (const label of [...NAV, "Create"]) {
    await expect(sheet.getByRole("link", { name: label, exact: true })).toBeVisible();
  }
  await page.keyboard.press("Escape");
  await expect(sheet).toBeHidden();
});

test("footer lists the site's links and a theme selector", async ({ page }) => {
  await page.goto("/forum");
  const footer = page.getByRole("contentinfo");
  for (const label of ["Explore", "Create", "Forum", "Leaderboard", "Analytics", "Docs"]) {
    await expect(footer.getByRole("link", { name: label, exact: true })).toBeAttached();
  }
  const theme = footer.getByRole("group", { name: "Color theme" });
  await expect(theme.getByRole("button", { name: "Light" })).toBeAttached();
  await theme.getByRole("button", { name: "Light" }).click();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
});
