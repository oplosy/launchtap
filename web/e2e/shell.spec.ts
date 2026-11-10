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

test("footer lists the site's links and no theme control", async ({ page }) => {
  await page.goto("/forum");
  const footer = page.getByRole("contentinfo");
  for (const label of ["Explore", "Create", "Forum", "Leaderboard", "Analytics", "Docs"]) {
    await expect(footer.getByRole("link", { name: label, exact: true })).toBeAttached();
  }
  await expect(page.getByRole("group", { name: "Color theme" })).toHaveCount(0);
  await expect(footer.getByRole("button", { name: /theme|light|dark|system/i })).toHaveCount(0);
});

test("one header button flips between the dark and light themes", async ({
  page,
  context,
  baseURL,
}) => {
  await context.addCookies([{ name: "launchpad-theme", value: "dark", url: baseURL! }]);
  await page.goto("/forum");
  const html = page.locator("html");
  const toggle = page.getByRole("banner").getByRole("button", { name: "Switch to light theme" });
  await expect(toggle).toBeVisible();
  await toggle.click();
  await expect(html).toHaveAttribute("data-theme", "light");
  const back = page.getByRole("banner").getByRole("button", { name: "Switch to dark theme" });
  await back.click();
  await expect(html).toHaveAttribute("data-theme", "dark");
  await page.reload();
  await expect(html).toHaveAttribute("data-theme", "dark");
});

test("dropdown options follow the active theme", async ({ page, context, baseURL }) => {
  for (const theme of ["dark", "light"] as const) {
    await context.addCookies([{ name: "launchpad-theme", value: theme, url: baseURL! }]);
    await page.goto("/explore");
    const select = page.locator("select").first();
    await expect(select).toBeAttached();
    const colors = await select.evaluate((node) => {
      const option = getComputedStyle(node.querySelector("option")!);
      const body = getComputedStyle(document.body);
      // A customizable select draws its own panel; the native list paints each option.
      const panel = CSS.supports("appearance", "base-select")
        ? getComputedStyle(node, "::picker(select)")
        : option;
      return {
        optionBackground: panel.backgroundColor,
        optionText: option.color,
        canvas: body.backgroundColor,
        text: body.color,
      };
    });
    expect(colors.optionBackground, theme).toBe(colors.canvas);
    expect(colors.optionText, theme).toBe(colors.text);
  }
});

test("dropdowns open into a rounded, animated picker", async ({ page }) => {
  await page.goto("/explore");
  const select = page.locator("select").first();
  await expect(select).toBeAttached();
  const style = await select.evaluate((node) => {
    const picker = getComputedStyle(node, "::picker(select)");
    return {
      supported: CSS.supports("appearance", "base-select"),
      appearance: getComputedStyle(node).appearance,
      radius: parseFloat(picker.borderTopLeftRadius),
      transition: picker.transitionProperty,
    };
  });
  test.skip(!style.supported, "This browser draws the native list.");
  expect(style.appearance).toBe("base-select");
  expect(style.radius).toBeGreaterThanOrEqual(12);
  expect(style.transition).toContain("opacity");
});
