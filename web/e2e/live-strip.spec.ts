import { expect, test, type Page } from "@playwright/test";

// The strip renders only with a ready configuration, which the E2E fixture build provides when
// the TASK6_ANVIL_* variables are set (any valid addresses and a URL; the API is mocked here).
test.skip(
  !process.env.TASK6_ANVIL_API_URL,
  "Needs the fixture build (TASK6_ANVIL_* variables) so the configuration is ready.",
);

const CHAIN_ID = 31337;
const DEPLOYMENT_ID = "task6-anvil";
const snapshot = {
  chain_id: CHAIN_ID,
  as_of_block: 10,
  as_of_block_hash: "0xabc",
  finality: "safe",
};
const longName = "🚀".repeat(64);
const tokenA = "0x00000000000000000000000000000000000000Aa";

function trade(n: number, over: Record<string, unknown> = {}) {
  return {
    chain_id: CHAIN_ID,
    deployment_id: DEPLOYMENT_ID,
    token: tokenA,
    name: longName,
    symbol: "🚀".repeat(20),
    source: "curve",
    trader: null,
    side: "buy",
    eth_volume: "1500000000000000000",
    token_volume: "5000000000000000000",
    block_number: n,
    transaction_index: 0,
    tx_hash: `0x${n.toString(16).padStart(64, "0")}`,
    log_index: 0,
    time: new Date().toISOString(),
    finality: "observed",
    ...over,
  };
}

async function installApi(page: Page) {
  await page.route("**/v1/**", async (route) => {
    const url = new URL(route.request().url());
    if (url.pathname.endsWith("/events")) {
      await route.fulfill({ status: 200, contentType: "text/event-stream", body: "" });
    } else if (url.pathname.endsWith("/trades/recent")) {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ snapshot, items: [trade(2), trade(1, { side: "sell" })] }),
      });
    } else if (url.pathname.endsWith("/eth-usd")) {
      await route.fulfill({ status: 404, contentType: "application/json", body: "{}" });
    } else {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ snapshot, items: [], next_cursor: undefined }),
      });
    }
  });
}

test("live strip lists recent trades, links to the token and truncates long names", async ({
  page,
}) => {
  await installApi(page);
  await page.goto("/");
  const strip = page.getByRole("region", { name: "Live activity" });
  await expect(strip).toBeVisible();
  const items = strip.locator("li");
  await expect(items).toHaveCount(2);
  await expect(items.first().getByRole("link")).toHaveAttribute(
    "href",
    `/token/${tokenA.toLowerCase()}`,
  );
  // An item is the token, then direction and size as one value coloured by side; the trader
  // and the trade's age are not shown.
  const value = items.first().locator(".live-strip-value");
  await expect(value).toHaveAttribute("data-side", "buy");
  await expect(value).toContainText("1.5 ETH");
  await expect(items.first()).not.toContainText("—");
  const ticker = items.first().locator(".live-strip-ticker");
  const style = await ticker.evaluate((element) => {
    const computed = getComputedStyle(element);
    return {
      overflow: computed.overflow,
      textOverflow: computed.textOverflow,
      truncated: element.scrollWidth > element.clientWidth,
    };
  });
  expect(style).toEqual({ overflow: "hidden", textOverflow: "ellipsis", truncated: true });
  // Observed items read as pending until finality.
  await expect(items.first()).toHaveAttribute("data-pending", "true");
  // Each item carries the token image (here the mocked image route fails, so the letter
  // fallback) in a fixed 20x20 box, so the row height does not depend on the image.
  const image = items.first().locator(".live-strip-image-box");
  await expect(image).toHaveAttribute("aria-hidden", "true");
  await expect(image.locator(".live-strip-image")).toHaveCount(1);
  const box = await image.boundingBox();
  expect(box && { width: Math.round(box.width), height: Math.round(box.height) }).toEqual({
    width: 20,
    height: 20,
  });
});

test("live strip is a static scrollable row under reduced motion", async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== "reduced-motion", "Reduced-motion project only.");
  // Mirrors release-hardening: the project option alone is not honoured by every version.
  await page.emulateMedia({ reducedMotion: "reduce" });
  await installApi(page);
  await page.goto("/");
  const track = page.locator(".live-strip-track");
  await expect(track).toBeVisible();
  const result = await track.evaluate((element) => ({
    overflowX: getComputedStyle(element).overflowX,
    animated: [...element.querySelectorAll("li")].some(
      (li) => getComputedStyle(li).animationName !== "none",
    ),
  }));
  expect(result).toEqual({ overflowX: "auto", animated: false });
});

/** Replaces EventSource with a fake the test drives through `window.__sse`. */
async function installFakeEventSource(page: Page) {
  await page.addInitScript(() => {
    const sources: Array<EventTarget & { readyState: number }> = [];
    class FakeEventSource extends EventTarget {
      readyState = 0;
      constructor(readonly url: string) {
        super();
        sources.push(this);
        setTimeout(() => {
          if (this.readyState === 2) return;
          this.readyState = 1;
          this.dispatchEvent(new Event("open"));
        }, 0);
      }
      close() {
        this.readyState = 2;
      }
    }
    Object.assign(window, {
      EventSource: FakeEventSource,
      __sse: {
        open: () => sources.filter((source) => source.readyState === 1).length,
        emit: (name: string, data: unknown) => {
          const source = sources.filter((candidate) => candidate.readyState === 1).at(-1);
          source?.dispatchEvent(new MessageEvent(name, { data: JSON.stringify(data) }));
        },
      },
    });
  });
}

type SseHandle = { open: () => number; emit: (name: string, data: unknown) => void };

const TOKEN_PAGE = "0x00000000000000000000000000000000000000bb";
const wad = (value: string) => `${BigInt(value) * 10n ** 18n}`;

async function installTokenApi(page: Page) {
  await page.route("**/v1/**", async (route) => {
    const url = new URL(route.request().url());
    const empty = { snapshot, items: [], next_cursor: undefined };
    if (url.pathname.endsWith("/eth-usd") || url.pathname.endsWith("/image")) {
      await route.fulfill({ status: 404, contentType: "application/json", body: "{}" });
    } else if (url.pathname.endsWith(`/tokens/${TOKEN_PAGE}`)) {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          snapshot,
          address: TOKEN_PAGE,
          ath_at: "2026-09-10T13:00:00Z",
          ath_price_eth: wad("1"),
          creator: TOKEN_PAGE,
          curve: TOKEN_PAGE,
          curve_tokens: wad("1000"),
          description: "",
          engine_version: 1,
          eth_reserve: wad("5"),
          fdv_eth: wad("100"),
          graduation_eth: wad("70"),
          graduation_progress_bps: 700,
          holder_count: 1,
          image_url: "",
          initial_virtual_eth: wad("1"),
          initial_virtual_token: wad("1000"),
          liquidity_eth: wad("5"),
          lp_tokens: "0",
          market_cap_eth: wad("100"),
          name: "Reorg Route",
          pair: "",
          phase: "curve",
          price_change_24h_bps: 0,
          protocol_share_bps: 100,
          protocol_treasury: TOKEN_PAGE,
          real_curve_eth: wad("5"),
          reserve_block: 10,
          reserve_hash: "0xabc",
          reserve_source: "indexed",
          spot_price_eth: wad("1"),
          symbol: "RORG",
          telegram_url: "",
          token_reserve: wad("900"),
          total_supply: wad("1000"),
          trade_fee_bps: 100,
          volume_24h_eth: "0",
          weth: TOKEN_PAGE,
          x_url: "",
        }),
      });
    } else {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(empty),
      });
    }
  });
}

test("a live trade row is priced from its own amounts and a reorg removes it", async ({
  page,
}, testInfo) => {
  test.skip(testInfo.project.name !== "small-laptop", "One viewport is enough.");
  await installFakeEventSource(page);
  await installTokenApi(page);
  await page.goto(`/token/${TOKEN_PAGE}`);
  const table = page.getByRole("table", { name: "Recent trades" });
  await expect(page.getByText("No trades yet")).toBeVisible();
  await page.waitForFunction(
    () => ((window as unknown as { __sse?: SseHandle }).__sse?.open() ?? 0) > 0,
  );
  const emit = (name: string, data: unknown) =>
    page.evaluate(
      ([eventName, payload]) =>
        (window as unknown as { __sse: SseHandle }).__sse.emit(eventName as string, payload),
      [name, data] as const,
    );
  // 1.5 ETH for 5 tokens: 0.3 ETH per token, as REST would report; spot is unknown live.
  await emit("trade", trade(12, { token: TOKEN_PAGE, trader: TOKEN_PAGE }));
  const rows = table.locator(".history-row:not(.history-heading)");
  await expect(rows).toHaveCount(1);
  await expect(rows.first().locator(".trade-prices")).toContainText("0.3 / —");

  // A reorg back to block 11 drops the live row from block 12.
  await emit("reorg", { chain_id: CHAIN_ID, deployment_id: DEPLOYMENT_ID, common_ancestor: 11 });
  await expect(page.getByText("No trades yet")).toBeVisible();
  await expect(table).toHaveCount(0);
});
