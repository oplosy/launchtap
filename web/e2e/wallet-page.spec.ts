import http from "node:http";
import { expect, test, type Page } from "@playwright/test";

// Like live-strip.spec.ts: every API call is mocked with page.route, so the page only needs the
// READY fixture configuration (TASK6_ANVIL_* variables).
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
const WALLET = "0x00000000000000000000000000000000000000ab";
const TOKEN_OPEN = "0x0000000000000000000000000000000000000001";
const TOKEN_CLOSED = "0x0000000000000000000000000000000000000002";
const ETH = 10n ** 18n;

type State = { attributionComplete: boolean; calls: Record<string, number> };

function position(token: string, symbol: string, over: Record<string, unknown> = {}) {
  return {
    token,
    name: `${symbol} token`,
    symbol,
    phase: "curve",
    balance: (3n * ETH).toString(),
    average_cost_wad: (ETH / 10n).toString(),
    remaining_cost_wei: (3n * (ETH / 10n)).toString(),
    realized_pnl_wei: "0",
    current_value_wei: ETH.toString(),
    unrealized_pnl_wei: "700000000000000000",
    transferred_in: "0",
    trade_count: 2,
    last_block: 9,
    ...over,
  };
}

function walletTrade(n: number) {
  return {
    token: TOKEN_OPEN,
    name: "OPEN token",
    symbol: "OPEN",
    source: "curve",
    trader: WALLET,
    side: "buy",
    execution_price: "1",
    spot_price: "1",
    eth_volume: ETH.toString(),
    token_volume: (5n * ETH).toString(),
    block_number: n,
    transaction_index: 0,
    tx_hash: `0x${n.toString(16).padStart(64, "0")}`,
    log_index: 0,
    time: new Date().toISOString(),
    finality: "safe",
  };
}

function stripTrade(n: number) {
  return {
    chain_id: CHAIN_ID,
    deployment_id: DEPLOYMENT_ID,
    token: TOKEN_OPEN,
    name: "OPEN token",
    symbol: "OPEN",
    source: "curve",
    trader: WALLET,
    side: "buy",
    eth_volume: ETH.toString(),
    token_volume: (5n * ETH).toString(),
    block_number: n,
    transaction_index: 0,
    tx_hash: `0x${n.toString(16).padStart(64, "0")}`,
    log_index: 0,
    time: new Date().toISOString(),
    finality: "safe",
  };
}

async function installApi(page: Page, state: State) {
  await page.route("**/v1/**", async (route) => {
    const url = new URL(route.request().url());
    const path = url.pathname.replace(/^.*\/v1/, "");
    state.calls[path] = (state.calls[path] ?? 0) + 1;
    const json = (body: unknown, status = 200) =>
      route.fulfill({ status, contentType: "application/json", body: JSON.stringify(body) });
    if (path === `/wallets/${WALLET}`) {
      return json({
        snapshot,
        address: WALLET,
        realized_pnl_wei: "200000000000000000",
        unrealized_pnl_wei: "-500000000000000000",
        portfolio_value_wei: (2n * ETH).toString(),
        held_count: 1,
        created_count: 1,
        tokens_traded: 2,
        first_block: 1,
        last_block: 9,
        attribution_complete: state.attributionComplete,
      });
    }
    if (path === `/wallets/${WALLET}/positions`) {
      const closed = url.searchParams.get("closed") === "true";
      return json({
        snapshot,
        truncated: false,
        items: closed
          ? [
              position(TOKEN_CLOSED, "GONE", {
                balance: "0",
                average_cost_wad: null,
                remaining_cost_wei: "0",
                current_value_wei: "0",
                unrealized_pnl_wei: "0",
                realized_pnl_wei: "300000000000000000",
              }),
            ]
          : [position(TOKEN_OPEN, "OPEN", { transferred_in: (ETH / 2n).toString() })],
      });
    }
    if (path === `/wallets/${WALLET}/trades`)
      return json({ snapshot, items: [walletTrade(2), walletTrade(1)] });
    if (path === `/wallets/${WALLET}/created`)
      return json({
        snapshot,
        items: [
          {
            token: TOKEN_OPEN,
            name: "OPEN token",
            symbol: "OPEN",
            phase: "curve",
            market_cap_wei: (40n * ETH).toString(),
            launch_block: 1,
          },
        ],
      });
    if (path === "/trades/recent") return json({ snapshot, items: [stripTrade(2), stripTrade(1)] });
    if (path.endsWith("/eth-usd")) return json({}, 404);
    return json({ snapshot, items: [], next_cursor: undefined });
  });
}

/**
 * Replaces EventSource with a controllable fake so a test decides exactly which stream events
 * arrive and no reconnect cycle adds refetches. `window.__sseSources` counts opened streams.
 */
async function installFakeEventSource(page: Page) {
  await page.addInitScript(() => {
    type Listener = (event: { data: string }) => void;
    const sources: Array<{ url: string; listeners: Record<string, Listener[]> }> = [];
    class FakeEventSource {
      url: string;
      listeners: Record<string, Listener[]> = {};
      constructor(url: string) {
        this.url = url;
        sources.push(this);
      }
      addEventListener(type: string, listener: Listener) {
        (this.listeners[type] ??= []).push(listener);
      }
      close() {}
    }
    Object.assign(window, {
      EventSource: FakeEventSource,
      __sseSources: sources,
      __sseEmit: (type: string, data: unknown) => {
        for (const source of sources)
          for (const listener of source.listeners[type] ?? [])
            listener({ data: JSON.stringify(data) });
      },
    });
  });
}

function sseTrade(trader: string | null, n: number) {
  return { ...stripTrade(n), trader, finality: "observed" };
}

test("wallet page renders totals and holdings with a transfer badge", async ({ page }) => {
  const state: State = { attributionComplete: true, calls: {} };
  await installApi(page, state);
  await page.goto(`/wallet/${WALLET}`);
  const totals = page.getByRole("region", { name: "Wallet totals" });
  await expect(totals).toContainText("+0.2 ETH");
  await expect(totals).toContainText("−0.5 ETH");
  await expect(totals).toContainText("2 ETH");
  const holdings = page.getByRole("table", { name: "Holdings" });
  await expect(holdings).toContainText("OPEN");
  await expect(holdings.getByText("Transfer")).toBeVisible();
  await expect(page.getByText("PnL may be incomplete")).toHaveCount(0);
  await expect(
    page.getByText("Pool trades are attributed to the transaction sender."),
  ).toBeVisible();
  await expect(holdings.getByRole("link", { name: "OPEN" })).toHaveAttribute(
    "href",
    `/token/${TOKEN_OPEN}`,
  );
});

test("show closed reveals a closed row, and trades and created tabs load", async ({ page }) => {
  const state: State = { attributionComplete: true, calls: {} };
  await installApi(page, state);
  await page.goto(`/wallet/${WALLET}`);
  const holdings = page.getByRole("table", { name: "Holdings" });
  await expect(holdings).toContainText("OPEN");
  await expect(holdings).not.toContainText("GONE");
  await page.getByLabel("Show closed").check();
  await expect(holdings).toContainText("GONE");
  await expect(holdings.locator("[data-closed]")).toHaveCount(1);
  await page.getByRole("tab", { name: "Trades" }).click();
  await expect(page.getByRole("table", { name: "Wallet trades" }).getByRole("row")).toHaveCount(3);
  await page.getByRole("tab", { name: "Created" }).click();
  await expect(page.getByRole("table", { name: "Created tokens" })).toContainText("40 ETH");
});

test("incomplete attribution shows the banner", async ({ page }) => {
  const state: State = { attributionComplete: false, calls: {} };
  await installApi(page, state);
  await page.goto(`/wallet/${WALLET}`);
  await expect(page.getByRole("status").filter({ hasText: "PnL may be incomplete" })).toBeVisible();
});

test("a burst of live trades for this wallet causes exactly one overview refetch", async ({
  page,
}) => {
  const state: State = { attributionComplete: true, calls: {} };
  await installApi(page, state);
  await installFakeEventSource(page);
  await page.goto(`/wallet/${WALLET}`);
  await expect(page.getByRole("region", { name: "Wallet totals" })).toBeVisible();
  const overview = `/wallets/${WALLET}`;
  expect(state.calls[overview]).toBe(1);
  // One shared stream, not a second EventSource for the wallet page.
  expect(
    await page.evaluate(() => (window as never as { __sseSources: unknown[] }).__sseSources.length),
  ).toBe(1);
  await page.evaluate(
    ([a, b, c, other]) => {
      const emit = (window as never as { __sseEmit: (t: string, d: unknown) => void }).__sseEmit;
      emit("trade", a);
      emit("trade", b);
      emit("trade", other); // another wallet: must not count
      emit("trade", c);
    },
    [
      sseTrade(WALLET.toUpperCase().replace("0X", "0x"), 3),
      sseTrade(WALLET, 4),
      sseTrade(WALLET, 5),
      sseTrade("0x00000000000000000000000000000000000000cd", 6),
    ] as const,
  );
  await expect.poll(() => state.calls[overview]).toBe(2);
  // Well past the 500 ms coalescing window: still exactly one extra fetch.
  await page.waitForTimeout(1500);
  expect(state.calls[overview]).toBe(2);
});

test("a trade for another wallet does not refetch", async ({ page }) => {
  const state: State = { attributionComplete: true, calls: {} };
  await installApi(page, state);
  await installFakeEventSource(page);
  await page.goto(`/wallet/${WALLET}`);
  await expect(page.getByRole("region", { name: "Wallet totals" })).toBeVisible();
  await page.evaluate(
    (trade) => {
      (window as never as { __sseEmit: (t: string, d: unknown) => void }).__sseEmit("trade", trade);
    },
    sseTrade("0x00000000000000000000000000000000000000cd", 7),
  );
  await page.waitForTimeout(1000);
  expect(state.calls[`/wallets/${WALLET}`]).toBe(1);
});

test("a strip trader link opens that wallet page", async ({ page }) => {
  const state: State = { attributionComplete: true, calls: {} };
  await installApi(page, state);
  await page.goto("/");
  const strip = page.getByRole("region", { name: "Live activity" });
  const link = strip.locator(`a[href="/wallet/${WALLET}"]`).first();
  await expect(link).toBeVisible();
  await link.click();
  await expect(page).toHaveURL(new RegExp(`/wallet/${WALLET}$`));
  await expect(page.getByRole("region", { name: "Wallet totals" })).toBeVisible();
});

test("an invalid wallet address renders the not-found page", async ({ page }) => {
  const state: State = { attributionComplete: true, calls: {} };
  await installApi(page, state);
  const response = await page.goto("/wallet/not-an-address");
  expect(response?.status()).toBe(404);
  await expect(
    page.getByRole("heading", { name: "That launch route does not exist." }),
  ).toBeVisible();
});

test("the leaderboard lists ranks", async ({ page }) => {
  // The leaderboard is fetched by the Next server, so page.route cannot see it: serve the
  // fixture API origin the server was started with.
  const api = new URL(process.env.TASK6_ANVIL_API_URL ?? "");
  const server = http.createServer((request, response) => {
    if (request.url?.startsWith("/v1/leaderboard")) {
      response.writeHead(200, { "Content-Type": "application/json" });
      response.end(
        JSON.stringify({
          snapshot,
          items: [
            { rank: 1, address: WALLET, realized_pnl_wei: "1500000000000000000", tokens_traded: 4 },
            {
              rank: 2,
              address: "0x00000000000000000000000000000000000000cd",
              realized_pnl_wei: "-250000000000000000",
              tokens_traded: 2,
            },
          ],
        }),
      );
      return;
    }
    response.writeHead(404).end();
  });
  const listening = await new Promise<boolean>((resolve) => {
    server.once("error", () => resolve(false));
    server.listen(Number(api.port), api.hostname, () => resolve(true));
  });
  try {
    test.skip(!listening, "The fixture API port is already in use.");
    await page.goto("/leaderboard");
    const table = page.getByRole("table", { name: "Leaderboard" });
    await expect(table.getByRole("row")).toHaveCount(3);
    await expect(table.getByRole("row").nth(1)).toContainText("1");
    await expect(table.getByRole("row").nth(1)).toContainText("+1.5 ETH");
    await expect(table.getByRole("row").nth(2)).toContainText("−0.25 ETH");
    await expect(table.locator(`a[href="/wallet/${WALLET}"]`)).toBeVisible();
  } finally {
    await new Promise((resolve) => server.close(resolve));
  }
});

/** Flags any script chunk that embeds the Privy SDK, i.e. the wallet stack. */
function trackWalletStack(page: Page) {
  const hits: string[] = [];
  page.on("response", async (response) => {
    const url = response.url();
    if (!url.includes("/_next/static/chunks/") || !url.endsWith(".js")) return;
    try {
      if ((await response.text()).includes("auth.privy.io")) hits.push(url);
    } catch {
      // A response that is gone is not a wallet-stack load.
    }
  });
  return hits;
}

test("the wallet page does not load the wallet stack", async ({ page }) => {
  const state: State = { attributionComplete: true, calls: {} };
  await installApi(page, state);
  // The app-wide idle prefetch would warm the wallet-stack chunk on any route; silence it so a
  // chunk request can only come from the route itself requiring the stack.
  await page.addInitScript(() => {
    Object.assign(window, { requestIdleCallback: () => 0, cancelIdleCallback: () => undefined });
  });
  const control = trackWalletStack(page);
  await page.goto("/profile", { waitUntil: "load" });
  await expect
    .poll(() => control.length, { message: "detector must see the stack on /profile" })
    .toBeGreaterThan(0);

  const fresh = await page.context().newPage();
  await installApi(fresh, state);
  await fresh.addInitScript(() => {
    Object.assign(window, { requestIdleCallback: () => 0, cancelIdleCallback: () => undefined });
  });
  const hits = trackWalletStack(fresh);
  const connectionRequests: string[] = [];
  fresh.on("request", (request) => {
    if (/privy/i.test(request.url())) connectionRequests.push(request.url());
  });
  await fresh.goto(`/wallet/${WALLET}`, { waitUntil: "load" });
  await expect(fresh.getByRole("region", { name: "Wallet totals" })).toBeVisible();
  await fresh.waitForTimeout(1000);
  expect(hits).toEqual([]);
  expect(connectionRequests).toEqual([]);
  await fresh.close();
});
