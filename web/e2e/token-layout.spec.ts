import { expect, test, type Page } from "@playwright/test";

// Every API call is mocked with page.route, so the page only needs the READY fixture
// configuration (TASK6_ANVIL_* variables): the trade card renders only when it is ready.
test.skip(
  !process.env.TASK6_ANVIL_API_URL,
  "Needs the fixture build (TASK6_ANVIL_* variables) so the configuration is ready.",
);

const CHAIN_ID = 31337;
const DEPLOYMENT_ID = "task6-anvil";
const TOKEN = "0x00000000000000000000000000000000000000cc";
const snapshot = {
  chain_id: CHAIN_ID,
  as_of_block: 10,
  as_of_block_hash: "0xabc",
  finality: "safe",
};
const wad = (value: string) => `${BigInt(value) * 10n ** 18n}`;
// User-controlled text: markup in a name must show literally and never run.
const hostileName = "<img src=x onerror=window.__pwned=1>".padEnd(120, "W");

type State = { name: string; price: string };

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

function trade(n: number) {
  return {
    block_number: n,
    eth_volume: wad("1"),
    execution_price: wad("1"),
    finality: "safe",
    log_index: n,
    side: n % 2 ? "buy" : "sell",
    source: "curve",
    spot_price: wad("1"),
    time: "2026-11-09T12:00:00Z",
    token_volume: wad("1"),
    trader: TOKEN,
    transaction_index: 0,
    tx_hash: `0x${n.toString(16).padStart(64, "0")}`,
  };
}

async function installApi(page: Page, state: State) {
  await page.route("**/v1/**", async (route) => {
    const url = new URL(route.request().url());
    const json = (body: unknown, status = 200) =>
      route.fulfill({ status, contentType: "application/json", body: JSON.stringify(body) });
    if (url.pathname.endsWith("/eth-usd") || url.pathname.endsWith("/image")) return json({}, 404);
    if (url.pathname.endsWith("/watchers")) return json({ watch_count: 3 });
    if (url.pathname.endsWith("/comments")) return json({ items: [], comment_count: 0 });
    // Enough rows that the page is far taller than the viewport.
    if (url.pathname.endsWith("/trades"))
      return json({ snapshot, items: Array.from({ length: 40 }, (_, i) => trade(i + 1)) });
    if (url.pathname.endsWith(`/tokens/${TOKEN}`)) {
      return json({
        snapshot,
        address: TOKEN,
        ath_at: "2026-09-10T13:00:00Z",
        ath_price_eth: wad("1"),
        creator: TOKEN,
        curve: TOKEN,
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
        name: state.name,
        pair: "",
        phase: "curve",
        price_change_24h_bps: 0,
        protocol_share_bps: 100,
        protocol_treasury: TOKEN,
        real_curve_eth: wad("5"),
        reserve_block: 10,
        reserve_hash: "0xabc",
        reserve_source: "indexed",
        spot_price_eth: state.price,
        symbol: "CHAT",
        telegram_url: "",
        token_reserve: wad("900"),
        total_supply: wad("1000"),
        trade_fee_bps: 100,
        volume_24h_eth: "0",
        weth: TOKEN,
        x_url: "",
      });
    }
    return json({ snapshot, items: [], next_cursor: undefined });
  });
}

async function openTokenPage(page: Page, state: State) {
  await installFakeEventSource(page);
  await installApi(page, state);
  await page.goto(`/token/${TOKEN}`);
  await page.waitForFunction(
    () => ((window as unknown as { __sse?: { open(): number } }).__sse?.open() ?? 0) > 0,
  );
}

const tradeHeading = (page: Page) => page.getByRole("heading", { name: "Trade CHAT" });

test("the header is centred and shows name, symbol and price", async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== "small-laptop", "One viewport is enough.");
  await openTokenPage(page, { name: "Chatty", price: wad("1") });
  const title = page.getByRole("heading", { level: 1, name: "Chatty" });
  await expect(title).toBeVisible();
  const header = page.locator(".token-header");
  await expect(header.getByText("CHAT", { exact: true })).toBeVisible();
  await expect(header.locator(".market-price-value")).toContainText("1");
  const box = (await title.boundingBox())!;
  expect(Math.abs(box.x + box.width / 2 - 640)).toBeLessThanOrEqual(2);
  await page.screenshot({ path: "../.impeccable/review/rebuild-token-wide.png" });
});

test("the trade card stays in view while the page scrolls at 1280 px", async ({
  page,
}, testInfo) => {
  test.skip(testInfo.project.name !== "small-laptop", "One viewport is enough.");
  await openTokenPage(page, { name: "Chatty", price: wad("1") });
  await expect(tradeHeading(page)).toBeVisible();
  await expect(page.getByRole("table", { name: "Recent trades" })).toBeVisible();
  await page.mouse.wheel(0, 1500);
  await expect.poll(() => page.evaluate(() => window.scrollY)).toBeGreaterThanOrEqual(1000);
  await expect(tradeHeading(page)).toBeInViewport();
  await page.screenshot({ path: "../.impeccable/review/rebuild-token-scrolled.png" });
});

test("at 360 px the Buy bar button opens the trade card as a Trade dialog", async ({
  page,
}, testInfo) => {
  test.skip(testInfo.project.name !== "mobile", "Mobile only.");
  await openTokenPage(page, { name: "Chatty", price: wad("1") });
  const bar = page.getByRole("region", { name: "Quick trade" });
  await bar.getByRole("button", { name: "Buy" }).click();
  const dialog = page.getByRole("dialog", { name: "Trade" });
  await expect(dialog).toBeVisible();
  await expect(dialog.getByRole("tab", { name: "Buy" })).toHaveAttribute("aria-selected", "true");
  await expect(dialog.getByLabel("ETH input")).toBeVisible();
  // One trade card: the dialog is that card, never a second copy of it.
  await expect(page.getByLabel("ETH input")).toHaveCount(1);
  await page.screenshot({
    path: "../.impeccable/review/rebuild-token-sheet-mobile.png",
    animations: "disabled",
  });
  await page.keyboard.press("Escape");
  await expect(dialog).toBeHidden();
  await bar.getByRole("button", { name: "Sell" }).click();
  await expect(
    page.getByRole("dialog", { name: "Trade" }).getByRole("tab", { name: "Sell" }),
  ).toHaveAttribute("aria-selected", "true");
});

test("a 120-character hostile name renders as text without horizontal overflow", async ({
  page,
}, testInfo) => {
  test.skip(testInfo.project.name !== "mobile", "Mobile only.");
  await openTokenPage(page, { name: hostileName, price: wad("1") });
  await expect(page.getByRole("heading", { level: 1 })).toHaveText(hostileName);
  expect(await page.evaluate(() => (window as unknown as { __pwned?: number }).__pwned)).toBe(
    undefined,
  );
  await expect
    .poll(() => page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth))
    .toBe(0);
});

async function recordFlashes(page: Page) {
  await page.evaluate(() => {
    const seen: string[] = [];
    Object.assign(window, { __flashes: seen });
    new MutationObserver(() => {
      const value = document.querySelector(".market-price-value")?.getAttribute("data-flash");
      if (value) seen.push(value);
    }).observe(document.body, { subtree: true, attributes: true, childList: true });
  });
}

const flashes = (page: Page) =>
  page.evaluate(() => (window as unknown as { __flashes: string[] }).__flashes);

const emitTokenHint = (page: Page) =>
  page.evaluate(
    (data) =>
      (window as unknown as { __sse: { emit(name: string, data: unknown): void } }).__sse.emit(
        "token",
        data,
      ),
    { chain_id: CHAIN_ID, deployment_id: DEPLOYMENT_ID, token: TOKEN },
  );

test("a price change flashes once and settles", async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== "small-laptop", "One viewport is enough.");
  const state = { name: "Chatty", price: wad("1") };
  await openTokenPage(page, state);
  const price = page.locator(".market-price-value");
  await expect(price).toBeVisible();
  await recordFlashes(page);
  state.price = wad("2");
  await emitTokenHint(page);
  await expect.poll(() => flashes(page)).toContain("up");
  await expect(price).toContainText("2");
  // The hint clears itself 600 ms after the change.
  await expect(price).not.toHaveAttribute("data-flash", /./, { timeout: 1500 });
});

test("a price change does not flash under reduced motion", async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== "reduced-motion", "Reduced motion only.");
  await page.emulateMedia({ reducedMotion: "reduce" });
  const state = { name: "Chatty", price: wad("1") };
  await openTokenPage(page, state);
  const price = page.locator(".market-price-value");
  await expect(price).toBeVisible();
  await recordFlashes(page);
  state.price = wad("2");
  await emitTokenHint(page);
  await expect(price).toContainText("2");
  await page.waitForTimeout(700);
  expect(await flashes(page)).toEqual([]);
});
