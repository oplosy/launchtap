import { expect, test, type Page } from "@playwright/test";
import { randomBytes } from "node:crypto";
import path from "node:path";

const tokenAddress = "0x0000000000000000000000000000000000000001";
const snapshot = { chain_id: 4663, as_of_block: 123, as_of_block_hash: "0xabc", finality: "safe" };
const wad = (value: string) => `${BigInt(value) * 10n ** 18n}`;
const wadFraction = (numerator: bigint, denominator: bigint) =>
  `${(numerator * 10n ** 18n) / denominator}`;

async function installFixture(page: Page, tokenOverrides: Record<string, unknown> = {}) {
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
    if (url.pathname.endsWith("/created")) {
      // The fixture creator launched this token and one other that graduated.
      const created = (token: string, phase: string) => ({
        token,
        phase,
        name: "Created",
        symbol: "CRT",
        launch_block: 100,
        market_cap_wei: "0",
      });
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          snapshot,
          items: [
            created(tokenAddress, "curve"),
            created("0x0000000000000000000000000000000000000002", "graduated"),
          ],
        }),
      });
      return;
    }
    if (url.pathname.endsWith("/early-buyers")) {
      // Three early wallets; the creator and one other were delivered in one transaction and
      // got their first ETH from the same wallet; two of the three were funding-checked.
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          snapshot,
          window_seconds: 60,
          window_open: false,
          early_wallet_count: 3,
          early_balance: wad("300"),
          cluster_count: 1,
          funding_checked: 2,
          clusters: [
            {
              wallet_count: 2,
              balance: wad("250"),
              links: ["same_transaction", "funding_source"],
              includes_creator: true,
              wallets: [
                { address: "0x0000000000000000000000000000000000000002", balance: wad("150") },
                { address: tokenAddress, balance: wad("100") },
              ],
            },
          ],
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
        // The one fixture holder first acquired at block 120, the launch block.
        launch_block: 120,
        launch_time: "2026-09-10T12:00:00.000000Z",
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
        ...tokenOverrides,
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
  // Engine-1 launch facts: graduation seeds 70 ETH against 10 tokens out of a 1,000 supply.
  const launchFactsPanel = page.locator(".token-launch-facts");
  await expect(launchFactsPanel.getByRole("heading", { name: "Launch facts" })).toBeVisible();
  await expect(launchFactsPanel).toContainText("1% to the protocol, 99% to the creator");
  await expect(launchFactsPanel).toContainText("Left to graduate");
  await launchFactsPanel.screenshot({
    path: path.resolve(process.cwd(), "../.impeccable/review/v1-launch-facts.png"),
  });
  await expect(page.locator(".token-creator")).toHaveText(
    "Created by 0x000000…000001. Launched 1 other token on Launchpad; 1 graduated.",
  );
  await page.getByRole("tab", { name: "Holders" }).click();
  await expect(page.getByRole("table", { name: "Token holders" })).toContainText("100");
  // One holder of 100 out of the 1,000 supply.
  await expect(page.locator(".holder-concentration").first()).toHaveText(
    /^Top 1 holder owns 10\.00% of the supply/,
  );
  await expect(page.locator(".holder-concentration").nth(1)).toHaveText(
    "1 holder first got in at the launch block (bundled with the launch, including any launch buy by the creator) and still holds 10.00% of the supply.",
  );
  const earlyBuyers = page.getByRole("region", { name: "Linked early wallets" });
  await expect(earlyBuyers).toContainText(
    "3 wallets got tokens from the curve in the first 60 seconds and hold 30.00% of the supply now.",
  );
  const group = earlyBuyers.locator("details").first();
  await expect(group.locator("summary")).toHaveText(
    "2 wallets hold 25.00% of the supply: received tokens in the same transaction and funded by the same wallet Creator",
  );
  await expect(earlyBuyers).toContainText("Funding sources checked for 2 of 3 early wallets.");
  await group.locator("summary").click();
  await expect(group.getByRole("link")).toHaveCount(2);
  await earlyBuyers.screenshot({
    path: path.resolve(process.cwd(), "../.impeccable/review/early-buyers.png"),
  });
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

function pngSize(body: Buffer) {
  // PNG signature (8 bytes) + IHDR length/type (8 bytes), then width and height as uint32 BE.
  expect(body.subarray(0, 8).toString("hex")).toBe("89504e470d0a1a0a");
  return { width: body.readUInt32BE(16), height: body.readUInt32BE(20) };
}

test("GET /token/<known>/opengraph-image returns 200 image/png 1200x630", async ({ request }) => {
  const response = await request.get(`/token/${tokenAddress}/opengraph-image`);
  expect(response.status()).toBe(200);
  expect(response.headers()["content-type"]).toContain("image/png");
  expect(pngSize(await response.body())).toEqual({ width: 1200, height: 630 });
});

test("unknown token returns 200 generic card", async ({ request }) => {
  for (const [address, cacheControl] of [
    // The API answered 404 (maybe a token seconds after launch): cached downstream only briefly.
    ["0x00000000000000000000000000000000000000ff", "public, s-maxage=30"],
    // An invalid address is a stable answer: the normal lifetime.
    ["not-an-address", "public, s-maxage=300, stale-while-revalidate=300"],
  ]) {
    const response = await request.get(`/token/${address}/opengraph-image`);
    expect(response.status()).toBe(200);
    expect(response.headers()["content-type"]).toContain("image/png");
    expect(response.headers()["cache-control"]).toBe(cacheControl);
    expect(pngSize(await response.body())).toEqual({ width: 1200, height: 630 });
  }
});

test("token page has og:image, twitter:card and a token title", async ({ request }) => {
  const response = await request.get(`/token/${tokenAddress}`);
  expect(response.status()).toBe(200);
  const html = await response.text();
  expect(html).toMatch(/<title>\$FIX - Fixture Route \| Launchpad<\/title>/);
  expect(html).toMatch(/<meta property="og:image" content="[^"]*\/opengraph-image[^"]*"/);
  expect(html).toContain('<meta name="twitter:card" content="summary_large_image"/>');
  expect(html).toMatch(/<meta name="twitter:image" content="[^"]*\/twitter-image[^"]*"/);
  expect(html).toMatch(/<link rel="canonical" href="[^"]*\/token\/0x0{39}1"/);
});

const mockApi = "http://127.0.0.1:3418";
const addressWithTail = (tail: string) => `0x${"1".repeat(36)}${tail}`;

test("emoji, CJK and all-emoji names still render a 1200x630 card", async ({ request }) => {
  for (const tail of ["0002", "0003", "0004"]) {
    const response = await request.get(`/token/${addressWithTail(tail)}/opengraph-image`);
    expect(response.status()).toBe(200);
    expect(response.headers()["content-type"]).toContain("image/png");
    expect(pngSize(await response.body())).toEqual({ width: 1200, height: 630 });
  }
});

for (const route of ["opengraph-image", "twitter-image"]) {
  test(`${route} is cached for 300s and reads the API once per window`, async ({ request }) => {
    const address = `0x${randomBytes(20).toString("hex")}`;
    const first = await request.get(`/token/${address}/${route}`);
    const second = await request.get(`/token/${address}/${route}`);
    expect(first.status()).toBe(200);
    expect(second.status()).toBe(200);
    const cacheControl = first.headers()["cache-control"] ?? "";
    expect(cacheControl).toContain("public");
    expect(cacheControl).toContain("s-maxage=300");
    const counts = (await (await request.get(`${mockApi}/__calls`)).json()) as Record<
      string,
      number
    >;
    expect(counts[`/v1/tokens/${address}`]).toBe(1);
    expect(counts[`/v1/tokens/${address}/image`]).toBe(1);
  });
}

test("token page metadata does not download the image or read the price", async ({ request }) => {
  // A fresh address per run: the Next data cache outlives the server in .next/cache.
  const address = `0x${randomBytes(20).toString("hex")}`;
  const response = await request.get(`/token/${address}`);
  expect(response.status()).toBe(200);
  // Metadata streams after the shell for browser user agents; read the whole body first.
  expect(await response.text()).toContain(`Token ${address.slice(-4)}`);
  const counts = (await (await request.get(`${mockApi}/__calls`)).json()) as Record<string, number>;
  expect(counts[`/v1/tokens/${address}`]).toBeGreaterThanOrEqual(1);
  expect(counts[`/v1/tokens/${address}/image`]).toBeUndefined();
});

test("token page offers Share on X and Copy link", async ({ page }) => {
  await installFixture(page);
  await page.goto(`/token/${tokenAddress}?fixture=populated`);
  const share = page.getByRole("link", { name: "Share on X" });
  await expect(share).toBeVisible();
  const href = new URL((await share.getAttribute("href")) ?? "");
  expect(href.origin + href.pathname).toBe("https://x.com/intent/post");
  expect(href.searchParams.get("text")).toBe("$FIX on Launchpad");
  expect(href.searchParams.get("url")).toContain(`/token/${tokenAddress}`);
  await expect(page.getByRole("button", { name: "Copy link" })).toBeVisible();
});

test("a graduated v2 token shows its pool facts and no fake liquidity", async ({ page }) => {
  const recipient = "0x00000000000000000000000000000000000000a1";
  const poolId = `0x${"ab".repeat(32)}`;
  await installFixture(page, {
    engine_version: 2,
    phase: "graduated",
    liquidity_eth: "0",
    eth_reserve: "0",
    token_reserve: "0",
    v2: {
      anti_snipe: { start_bps: 2000, duration_seconds: 600, ends_at: "2026-09-10T12:10:00Z" },
      fee_recipient: recipient,
      pending_fee_recipient: null,
      lp_creator_share_bps: 5000,
      locker_owed: [{ account: recipient, eth: wad("1"), token: "0" }],
      pool: {
        pool_id: poolId,
        hook: "0x0000000000000000000000000000000000002000",
        position_token_id: "7",
      },
    },
  });
  await page.setViewportSize({ width: 1280, height: 900 });
  await page.goto(`/token/${tokenAddress}?fixture=populated`);
  const facts = page.getByRole("region", { name: "Engine v2" });
  await expect(facts).toBeVisible();
  await expect(facts).toContainText("20%, decaying to 0 over 10 min");
  await expect(facts).toContainText(poolId);
  await expect(facts).toContainText(recipient);
  await expect(page.getByText("Not indexed yet", { exact: true })).toBeVisible();
  await expect(page.getByText("ETH reserve")).toHaveCount(0);
});

test("an engine-1 token shows no v2 facts", async ({ page }) => {
  await installFixture(page);
  await page.goto(`/token/${tokenAddress}?fixture=populated`);
  await expect(page.getByRole("heading", { name: "Fixture Route" })).toBeVisible();
  await expect(page.getByRole("region", { name: "Engine v2" })).toHaveCount(0);
  await expect(page.getByText("ETH reserve")).toBeVisible();
});
