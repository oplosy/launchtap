import { expect, test, type Locator } from "@playwright/test";

// Motion: selection indicators slide between options, and overlays animate out as well as in.

const indicatorX = (group: Locator) =>
  group
    .locator(".slide-indicator")
    .evaluate((node) => (node as HTMLElement).style.getPropertyValue("--x"));

test("a segmented choice slides one indicator to the pressed option", async ({
  page,
}, testInfo) => {
  test.skip(testInfo.project.name !== "small-laptop", "One viewport is enough.");
  await page.goto("/explore");
  const group = page.getByRole("group", { name: "Layout" });
  const grid = group.getByRole("button", { name: "Grid" });
  const indicator = group.locator(".slide-indicator");
  await expect(indicator).toHaveAttribute("data-placed", "");
  const list = group.getByRole("button", { name: "List" });
  expect(await indicatorX(group)).toBe(
    `${await list.evaluate((n) => (n as HTMLElement).offsetLeft)}px`,
  );
  expect(await indicator.evaluate((node) => getComputedStyle(node).transitionProperty)).toContain(
    "transform",
  );
  await grid.click();
  const gridLeft = await grid.evaluate((n) => (n as HTMLElement).offsetLeft);
  await expect.poll(() => indicatorX(group)).toBe(`${gridLeft}px`);
});

test("view tabs slide their underline to the current view", async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== "small-laptop", "One viewport is enough.");
  await page.goto("/explore");
  const tabs = page.getByRole("navigation", { name: "Explore views" });
  await expect(tabs.locator(".slide-indicator")).toHaveAttribute("data-placed", "");
  const fresh = tabs.getByRole("link", { name: "New" });
  await fresh.click();
  await expect(fresh).toHaveAttribute("aria-current", "page");
  const left = await fresh.evaluate((n) => (n as HTMLElement).offsetLeft);
  await expect.poll(() => indicatorX(tabs)).toBe(`${left}px`);
});

test("a dialog animates out instead of vanishing", async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== "small-laptop", "One viewport is enough.");
  await page.goto("/explore");
  await page.getByRole("button", { name: "Search" }).click();
  const dialog = page.locator("dialog.command-palette");
  await expect(dialog).toBeVisible();
  // Closing starts an exit transition: the dialog stays rendered for its length.
  const displayRightAfterClose = await dialog.evaluate((node) => {
    (node as HTMLDialogElement).close();
    return getComputedStyle(node).display;
  });
  expect(displayRightAfterClose).not.toBe("none");
  await expect(dialog).toBeHidden();
});

test("reduced motion keeps indicators and dialogs still", async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== "small-laptop", "One viewport is enough.");
  await page.emulateMedia({ reducedMotion: "reduce" });
  await page.goto("/explore");
  const indicator = page.getByRole("group", { name: "Layout" }).locator(".slide-indicator");
  await expect(indicator).toHaveAttribute("data-placed", "");
  expect(await indicator.evaluate((node) => getComputedStyle(node).transitionProperty)).toBe(
    "none",
  );
  await page.getByRole("button", { name: "Search" }).click();
  const dialog = page.locator("dialog.command-palette");
  await expect(dialog).toBeVisible();
  const display = await dialog.evaluate((node) => {
    (node as HTMLDialogElement).close();
    return getComputedStyle(node).display;
  });
  expect(display).toBe("none");
});
