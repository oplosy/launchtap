import path from "node:path";

import budgetMatrix from "../performance-budgets.json" with { type: "json" };

import { expect, test, type Page } from "@playwright/test";

type RouteBudget = { transferredBytes: number; initialJavaScriptBytes: number };
const routeBudgets: Array<{ route: string; routeClass: string; budget: RouteBudget }> =
  Object.entries(budgetMatrix.routeClasses).flatMap(([routeClass, budget]) =>
    budget.routes.map((route) => ({ route, routeClass, budget })),
  );
const coreRoutes = routeBudgets.map(({ route }) => route);
const populatedTokenAddress = "0x0000000000000000000000000000000000000001";

test.beforeEach(async ({ page }, testInfo) => {
  // The project declares reducedMotion in playwright.config.ts; emulateMedia keeps
  // the preference effective on Playwright versions whose use-options type omits it.
  if (testInfo.project.name === "reduced-motion")
    await page.emulateMedia({ reducedMotion: "reduce" });
});

async function installPopulatedTokenFixture(page: Page) {
  await page.route("**/v1/**", async (route) => {
    const url = new URL(route.request().url());
    const snapshot = {
      chain_id: 4663,
      as_of_block: 123,
      as_of_block_hash: "0xabc",
      finality: "safe",
    };
    const wad = (value: string) => `${BigInt(value) * 10n ** 18n}`;
    if (url.pathname.endsWith("/events")) {
      await route.fulfill({ status: 200, contentType: "text/event-stream", body: "" });
    } else if (url.pathname.endsWith("/candles")) {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ snapshot, items: [] }),
      });
    } else if (url.pathname.endsWith("/trades")) {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ snapshot, items: [], next_cursor: undefined }),
      });
    } else if (url.pathname.endsWith("/holders")) {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ snapshot, items: [], next_cursor: undefined }),
      });
    } else {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          snapshot,
          address: populatedTokenAddress,
          name: "Budget Fixture",
          symbol: "BUD",
          creator: populatedTokenAddress,
          curve: populatedTokenAddress,
          pair: populatedTokenAddress,
          weth: populatedTokenAddress,
          protocol_treasury: populatedTokenAddress,
          phase: "curve",
          description: "Deterministic populated token for browser budgets.",
          engine_version: 1,
          image_url: null,
          curve_tokens: wad("1000"),
          eth_reserve: wad("2"),
          fdv_eth: wad("10"),
          graduation_eth: wad("70"),
          graduation_progress_bps: 100,
          holder_count: 0,
          initial_virtual_eth: wad("1"),
          initial_virtual_token: wad("1000"),
          liquidity_eth: wad("2"),
          lp_tokens: wad("0"),
          market_cap_eth: wad("10"),
          price_change_24h_bps: 0,
          protocol_share_bps: 100,
          real_curve_eth: wad("2"),
          reserve_block: 123,
          reserve_hash: "0xabc",
          reserve_source: "indexed",
          spot_price_eth: wad("1"),
          telegram_url: null,
          token_reserve: wad("900"),
          total_supply: wad("1000"),
          trade_fee_bps: 100,
          volume_24h_eth: wad("2"),
          ath_at: null,
          ath_price_eth: null,
        }),
      });
    }
  });
}

test("production responses expose hardened headers and safe caching", async ({ page }) => {
  const response = await page.goto("/");
  expect(response?.status()).toBe(200);
  const headers = response?.headers() ?? {};
  expect(headers["content-security-policy"]).toContain("default-src 'self'");
  expect(headers["content-security-policy"]).not.toMatch(/connect-src[^;]*\shttps:(?:\s|;)/);
  expect(headers["x-content-type-options"]).toBe("nosniff");
  expect(headers["x-frame-options"]).toBe("DENY");
  expect(headers["referrer-policy"]).toBe("strict-origin-when-cross-origin");
  expect(headers["cache-control"]).toContain("no-store");
});

test("missing routes use the first-class not-found boundary", async ({ page }) => {
  const response = await page.goto("/route-that-does-not-exist");
  expect(response?.status()).toBe(404);
  await expect(
    page.getByRole("heading", { name: "That launch route does not exist." }),
  ).toBeVisible();
  await expect(page.getByRole("link", { name: "Return to Explore" })).toBeVisible();
});

test("core routes stay within versioned profile navigation and transfer budgets", async ({
  page,
}, testInfo) => {
  const profile = budgetMatrix.profiles[testInfo.project.name as "small-laptop" | "mobile"];
  if (!profile) {
    test.skip(true, "The reduced-motion project has its own route-content suite.");
    return;
  }
  for (const { route, routeClass, budget } of routeBudgets) {
    await page.evaluate(() => performance.clearResourceTimings());
    if (route.includes("fixture=populated")) await installPopulatedTokenFixture(page);
    // Wait for load so every script the document requested is in the resource timeline.
    const response = await page.goto(route, { waitUntil: "load" });
    expect(response?.status(), route).toBe(200);
    const metrics = await page.evaluate(() => {
      const navigation = performance.getEntriesByType("navigation")[0] as
        PerformanceNavigationTiming | undefined;
      // Chunks requested after DOMContentLoaded are lazy (for example the idle wallet-stack
      // prefetch on read-only routes) and do not count toward the initial load.
      const initialCutoff = navigation?.domContentLoadedEventEnd ?? Number.POSITIVE_INFINITY;
      const resources = (
        performance.getEntriesByType("resource") as PerformanceResourceTiming[]
      ).filter(
        (resource) =>
          !resource.name.includes("/_next/static/chunks/") || resource.startTime <= initialCutoff,
      );
      const transferredBytes = resources.reduce(
        (sum, resource) =>
          sum +
          (resource.transferSize || resource.encodedBodySize || resource.decodedBodySize || 0),
        0,
      );
      const initialJavaScriptBytes = resources
        .filter(
          (resource) =>
            resource.name.includes("/_next/static/chunks/") && resource.name.endsWith(".js"),
        )
        .reduce(
          (sum, resource) =>
            sum +
            (resource.transferSize || resource.encodedBodySize || resource.decodedBodySize || 0),
          0,
        );
      return { navigationMs: navigation?.duration ?? 0, transferredBytes, initialJavaScriptBytes };
    });
    testInfo.annotations.push({
      type: "budget",
      description: `${routeClass} ${route} ${JSON.stringify(metrics)}`,
    });
    expect(metrics.navigationMs, `${route} navigation`).toBeLessThan(profile.navigationMs);
    expect(metrics.transferredBytes, `${route} transferred bytes`).toBeLessThan(
      budget.transferredBytes,
    );
    expect(metrics.initialJavaScriptBytes, `${route} initial JavaScript`).toBeLessThan(
      budget.initialJavaScriptBytes,
    );
  }
});

test("reduced motion covers core routes without clipping or animation dependence", async ({
  page,
}, testInfo) => {
  test.skip(testInfo.project.name !== "reduced-motion", "Reduced-motion project only.");
  for (const route of coreRoutes) {
    if (route.includes("fixture=populated")) await installPopulatedTokenFixture(page);
    await page.goto(route, { waitUntil: "domcontentloaded" });
    const result = await page.evaluate(() => {
      const visibleOverflow = [...document.querySelectorAll("body *")].some((element) => {
        const box = element.getBoundingClientRect();
        return (
          box.width > 0 && box.height > 0 && (box.left < -1 || box.right > window.innerWidth + 1)
        );
      });
      const animated = [...document.querySelectorAll("body *")].some((element) => {
        const style = getComputedStyle(element);
        const seconds = (value: string) =>
          value.endsWith("ms") ? Number.parseFloat(value) / 1000 : Number.parseFloat(value);
        return seconds(style.animationDuration) > 0.01 || seconds(style.transitionDuration) > 0.01;
      });
      return {
        horizontalOverflow: document.documentElement.scrollWidth > window.innerWidth,
        visibleOverflow,
        animated,
      };
    });
    expect(result.horizontalOverflow, `${route} horizontal overflow`).toBe(false);
    expect(result.visibleOverflow, `${route} clipped content`).toBe(false);
    expect(result.animated, `${route} reduced-motion animation`).toBe(false);
  }
});

test("mobile footer remains reachable below the final page content", async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== "mobile", "Mobile project only.");
  await page.goto("/create", { waitUntil: "domcontentloaded" });
  await page.evaluate(() => window.scrollTo(0, document.body.scrollHeight));
  const geometry = await page.evaluate(() => {
    const footer = document.querySelector(".app-footer")?.getBoundingClientRect();
    return {
      footerTop: footer?.top ?? 0,
      footerBottom: footer?.bottom ?? 0,
    };
  });
  expect(geometry.footerTop).toBeGreaterThanOrEqual(0);
  expect(geometry.footerBottom).toBeLessThanOrEqual(801);
});

test("reduced motion preserves route content without authored transition motion", async ({
  page,
}, testInfo) => {
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "Explore launches." })).toBeVisible();
  await page.screenshot({
    path: path.resolve(
      process.cwd(),
      "..",
      ".impeccable",
      "review",
      `task8-release-${testInfo.project.name}.png`,
    ),
    fullPage: true,
  });
  if (testInfo.project.name === "reduced-motion") {
    await expect(page.locator("html")).toHaveCSS("scroll-behavior", "auto");
    await expect(
      page.evaluate(() => window.matchMedia("(prefers-reduced-motion: reduce)").matches),
    ).resolves.toBe(true);
  }
});
