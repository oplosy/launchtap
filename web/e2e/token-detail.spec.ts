import { expect, test, type Page } from "@playwright/test";
import path from "node:path";

const tokenAddress = "0x0000000000000000000000000000000000000001";
const snapshot = { chain_id: 4663, as_of_block: 123, as_of_block_hash: "0xabc", finality: "safe" };
const wad = (value: string) => `${BigInt(value) * 10n ** 18n}`;
const wadFraction = (numerator: bigint, denominator: bigint) =>
  `${(numerator * 10n ** 18n) / denominator}`;

async function installFixture(page: Page) {
  const candleRequests: string[] = [];
  type FixtureCandle = {
    start: string;
    open: string;
    high: string;
    low: string;
    close: string;
    eth_volume: string;
    token_volume: string;
    trade_count: number;
  };
  const initialCandles: FixtureCandle[] = [
    {
      start: "2026-09-10T12:00:00Z",
      open: wad("1"),
      high: wad("2"),
      low: wad("1"),
      close: wad("2"),
      eth_volume: wad("3"),
      token_volume: wad("20"),
      trade_count: 2,
    },
    {
      start: "2026-09-10T13:00:00Z",
      open: wad("2"),
      high: wad("3"),
      low: wad("2"),
      close: wad("3"),
      eth_volume: wad("4"),
      token_volume: wad("25"),
      trade_count: 3,
    },
  ];
  let candleItems = initialCandles;
  await page.route("**/v1/**", async (route) => {
    const url = new URL(route.request().url());
    if (url.pathname.endsWith("/events")) {
      await route.fulfill({ status: 200, contentType: "text/event-stream", body: "" });
      return;
    }
    if (url.pathname.endsWith("/candles")) {
      candleRequests.push(url.search);
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          snapshot,
          items: candleItems,
        }),
      });
      return;
    }
    if (url.pathname.endsWith("/trades")) {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          snapshot,
          items: [
            {
              block_number: 123,
              eth_volume: wad("3"),
              execution_price: wad("2"),
              finality: "safe",
              log_index: 0,
              side: "buy",
              source: "curve",
              spot_price: wad("2"),
              time: "2026-09-10T13:00:00Z",
              token_volume: wad("10"),
              trader: tokenAddress,
              transaction_index: 0,
              tx_hash: "0xtrade",
            },
          ],
          next_cursor: undefined,
        }),
      });
      return;
    }
    if (url.pathname.endsWith("/holders")) {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          snapshot,
          items: [{ address: tokenAddress, balance: wad("100"), first_acquired_block: 120 }],
          next_cursor: undefined,
        }),
      });
      return;
    }
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        snapshot,
        address: tokenAddress,
        ath_at: "2026-09-10T13:00:00Z",
        ath_price_eth: wad("3"),
        creator: tokenAddress,
        curve: tokenAddress,
        curve_tokens: wad("1000"),
        description: "A fixture token description for browser verification.",
        engine_version: 1,
        eth_reserve: wad("50"),
        fdv_eth: wad("1000"),
        graduation_eth: wad("70"),
        graduation_progress_bps: 7143,
        holder_count: 1,
        image_url: "javascript:alert(1)",
        initial_virtual_eth: wad("1"),
        initial_virtual_token: wad("1000"),
        liquidity_eth: wad("50"),
        lp_tokens: wad("10"),
        market_cap_eth: wad("500"),
        name: "Fixture Route",
        pair: tokenAddress,
        phase: "curve",
        price_change_24h_bps: 125,
        protocol_share_bps: 100,
        protocol_treasury: tokenAddress,
        real_curve_eth: wad("50"),
        reserve_block: 123,
        reserve_hash: "0xabc",
        reserve_source: "indexed",
        spot_price_eth: wad("2"),
        symbol: "FIX",
        telegram_url: "https://t.me/fixture",
        token_reserve: wad("900"),
        total_supply: wad("1000"),
        trade_fee_bps: 100,
        volume_24h_eth: wad("20"),
        weth: tokenAddress,
        x_url: "https://x.com/fixture",
      }),
    });
  });
  return {
    candleRequests,
    setCandles(next: FixtureCandle[]) {
      candleItems = next;
    },
  };
}

test("malformed token address has a stable not-found state", async ({ page }) => {
  await page.goto("/token/not-an-address");
  await expect(page.getByRole("heading", { name: "Token not found" })).toBeVisible();
  await expect(page.getByRole("link", { name: "Return to Explore" })).toBeVisible();
});

test("fractional WAD fixture encodes 1.25 exactly", () => {
  expect(wadFraction(5n, 4n)).toBe("1250000000000000000");
});

test("valid token route fails closed when deployment is unavailable", async ({ page }) => {
  await page.setViewportSize({ width: 360, height: 800 });
  await page.goto("/token/0x0000000000000000000000000000000000000001");
  await expect(page.getByRole("heading", { name: "Token unavailable" })).toBeVisible();
  await expect(page.getByText("reviewed deployment is not configured")).toBeVisible();
  await expect(page.locator("body")).not.toContainText("Heatmap");
  await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth)).toBe(360);
});

test("token route remains readable with reduced motion", async ({ page }) => {
  await page.emulateMedia({ reducedMotion: "reduce" });
  await page.goto("/token/not-an-address");
  await expect(page.getByRole("heading", { name: "Token not found" })).toBeVisible();
});

test("populated token detail renders controls, pages, and responsive captures", async ({
  page,
}) => {
  const { candleRequests, setCandles } = await installFixture(page);
  await page.setViewportSize({ width: 1280, height: 900 });
  await page.goto(`/token/${tokenAddress}?fixture=populated`);
  await expect(page.getByRole("heading", { name: "Fixture Route" })).toBeVisible();
  await expect(page.getByText("Market and reserves")).toBeVisible();
  await expect(page.getByText("Spot price")).toBeVisible();
  await expect(page.getByText("Verified").first()).toBeVisible();
  await expect(page.getByText("About this token")).toBeVisible();
  await expect(page.getByRole("button", { name: "Candles" })).toBeVisible();
  await expect(page.getByRole("table", { name: "Recent trades" })).toContainText("buy");
  await expect.poll(() => candleRequests.length).toBe(1);
  expect(new URLSearchParams(candleRequests[0]).get("interval")).toBe("1h");
  expect(new URLSearchParams(candleRequests[0]).get("limit")).toBe("100");
  await page.getByLabel("Timeframe").selectOption("6h");
  await expect.poll(() => candleRequests.length).toBe(2);
  expect(new URLSearchParams(candleRequests[1]).get("interval")).toBe("6h");
  expect(new URLSearchParams(candleRequests[1]).get("limit")).toBe("100");
  await page.getByRole("tab", { name: "Holders" }).click();
  await expect(page.getByRole("table", { name: "Token holders" })).toContainText("100");
  await page.getByRole("tab", { name: "Recent trades" }).click();
  await page.getByRole("button", { name: "Candles" }).click();
  await expect(page.locator(".market-chart")).toBeVisible();
  const chart = page.locator(".market-chart");
  const initialChart = await chart.screenshot();
  const correctedCandles = [
    {
      start: "2026-09-10T12:00:00Z",
      open: wad("1"),
      high: wad("2"),
      low: wad("1"),
      close: wadFraction(5n, 4n),
      eth_volume: wad("8"),
      token_volume: wad("20"),
      trade_count: 2,
    },
    {
      start: "2026-09-10T13:00:00Z",
      open: wadFraction(5n, 4n),
      high: wad("3"),
      low: wadFraction(5n, 4n),
      close: wadFraction(5n, 2n),
      eth_volume: wad("1"),
      token_volume: wad("25"),
      trade_count: 3,
    },
  ];
  setCandles(correctedCandles);
  await page.getByLabel("Timeframe").selectOption("1d");
  await expect.poll(() => candleRequests.length).toBe(3);
  await expect.poll(async () => Buffer.compare(initialChart, await chart.screenshot())).not.toBe(0);
  const correctedChart = await chart.screenshot();
  setCandles(correctedCandles.slice(0, 1));
  await page.getByLabel("Timeframe").selectOption("all");
  await expect.poll(() => candleRequests.length).toBe(4);
  await expect
    .poll(async () => Buffer.compare(correctedChart, await chart.screenshot()))
    .not.toBe(0);
  await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth)).toBe(1280);
  await page.screenshot({
    path: path.resolve(process.cwd(), "../.impeccable/review/task5-token-wide.png"),
    fullPage: true,
  });
  await page.setViewportSize({ width: 768, height: 900 });
  await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth)).toBe(768);
  await page.screenshot({
    path: path.resolve(process.cwd(), "../.impeccable/review/task5-token-tablet.png"),
    fullPage: true,
  });
  await page.setViewportSize({ width: 360, height: 800 });
  await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth)).toBe(360);
  await page.screenshot({
    path: path.resolve(process.cwd(), "../.impeccable/review/task5-token-mobile.png"),
    fullPage: true,
  });
});
