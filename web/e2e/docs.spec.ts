import { expect, test } from "@playwright/test";

// Static pages: they render the same with or without a ready configuration.

test("docs read in a centred column of at most 820 px with contents beside it", async ({
  page,
}, testInfo) => {
  test.skip(testInfo.project.name !== "small-laptop", "One viewport is enough.");
  await page.goto("/docs");
  const article = page.locator(".docs-article");
  const contents = page.getByRole("navigation", { name: "Documentation sections" });
  // The contents stay hidden at the top of the page and fade in on the first scroll.
  await expect(contents).toBeHidden();
  await page.mouse.wheel(0, 200);
  await expect(contents).toBeVisible();
  await expect(page.getByRole("button", { name: "Contents" })).toBeHidden();
  const text = (await article.boundingBox())!;
  expect(text.width).toBeLessThanOrEqual(820);
  expect(Math.abs(text.x + text.width / 2 - 640)).toBeLessThanOrEqual(2);
  const nav = (await contents.boundingBox())!;
  expect(nav.x + nav.width).toBeLessThanOrEqual(text.x);
  await page.screenshot({ path: "../.impeccable/review/rebuild-docs-wide.png" });
});

test("at 360 px the contents collapse into a Contents button above the text", async ({
  page,
}, testInfo) => {
  test.skip(testInfo.project.name !== "mobile", "Mobile only.");
  await page.goto("/docs");
  const button = page.getByRole("button", { name: "Contents" });
  const contents = page.getByRole("navigation", { name: "Documentation sections" });
  await expect(button).toBeVisible();
  await expect(button).toHaveAttribute("aria-expanded", "false");
  await expect(contents).toBeHidden();
  const above = (await button.boundingBox())!;
  const text = (await page.locator(".docs-article").boundingBox())!;
  expect(above.y + above.height).toBeLessThanOrEqual(text.y);
  await button.click();
  await expect(button).toHaveAttribute("aria-expanded", "true");
  await expect(contents).toBeVisible();
  await contents.getByRole("link", { name: /Fees and economics/ }).click();
  await expect(page).toHaveURL(/#economics$/);
  await expect(contents).toBeHidden();
  await expect
    .poll(() => page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth))
    .toBe(0);
});

test("a missing route shows a centred heading and one link back", async ({ page }) => {
  const response = await page.goto("/does-not-exist");
  expect(response?.status()).toBe(404);
  const main = page.getByRole("main");
  const heading = main.getByRole("heading", { level: 1 });
  await expect(heading).toBeVisible();
  const box = (await heading.boundingBox())!;
  const width = page.viewportSize()!.width;
  expect(Math.abs(box.x + box.width / 2 - width / 2)).toBeLessThanOrEqual(2);
  await expect(main.getByRole("link")).toHaveCount(1);
  await expect(main.getByRole("link")).toHaveAttribute("href", "/explore");
  expect(await heading.evaluate((node) => getComputedStyle(node).textAlign)).toBe("center");
});

test("the docs describe the engine v2 rules", async ({ page }) => {
  await page.goto("/docs");
  const section = page.locator("#engine-v2");
  await expect(section.getByRole("heading", { name: "Engine v2 rules" })).toBeVisible();
  await expect(section).toContainText("Anti-snipe tax");
  await expect(section).toContainText("exempt from the anti-snipe tax");
  await expect(section).toContainText("locked forever");
  await expect(section).toContainText("two steps");
});
