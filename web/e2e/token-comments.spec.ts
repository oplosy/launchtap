import { expect, test, type Page } from "@playwright/test";

// Every API call is mocked with page.route, so the page only needs the READY fixture
// configuration (TASK6_ANVIL_* variables). The fixture build mounts no Privy provider, so these
// specs cover the reader paths: listing, live refresh, hidden comments and plain-text rendering.
// The signed-in composer paths are covered by the comment-model and comment-rules unit tests.
test.skip(
  !process.env.TASK6_ANVIL_API_URL,
  "Needs the fixture build (TASK6_ANVIL_* variables) so the configuration is ready.",
);

const CHAIN_ID = 31337;
const DEPLOYMENT_ID = "task6-anvil";
const TOKEN = "0x00000000000000000000000000000000000000cc";
const WALLET = "0x00000000000000000000000000000000000000a1";
const snapshot = {
  chain_id: CHAIN_ID,
  as_of_block: 10,
  as_of_block_hash: "0xabc",
  finality: "safe",
};
const wad = (value: string) => `${BigInt(value) * 10n ** 18n}`;

type SseHandle = { open: () => number; emit: (name: string, data: unknown) => void };
type Comment = {
  id: string;
  token: string;
  body: string;
  created_at: string;
  author: { username: string | null; wallet: string | null; role: "creator" | "holder" };
};

function comment(id: number, body: string, username: string | null = null): Comment {
  return {
    id: String(id),
    token: TOKEN,
    body,
    created_at: "2026-11-09T12:00:00.000000Z",
    author: { username, wallet: username ? null : WALLET, role: "holder" },
  };
}

type State = { comments: Comment[]; firstPageCalls: string[]; walletStack: number };

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

async function installApi(page: Page, state: State) {
  page.on("request", (request) => {
    if (/privy|walletconnect|reown/i.test(request.url())) state.walletStack += 1;
  });
  await page.route("**/v1/**", async (route) => {
    const url = new URL(route.request().url());
    const json = (body: unknown, status = 200) =>
      route.fulfill({ status, contentType: "application/json", body: JSON.stringify(body) });
    if (url.pathname.endsWith("/eth-usd") || url.pathname.endsWith("/image")) return json({}, 404);
    if (url.pathname.endsWith(`/tokens/${TOKEN}/watchers`)) return json({ watch_count: 1234 });
    if (url.pathname.endsWith(`/tokens/${TOKEN}/comments`)) {
      const cursor = url.searchParams.get("cursor");
      if (!cursor) state.firstPageCalls.push(route.request().url());
      const items = cursor ? state.comments.slice(2) : state.comments.slice(0, 2);
      return json({
        items,
        next_cursor: !cursor && state.comments.length > 2 ? "older" : undefined,
        comment_count: state.comments.length,
      });
    }
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
        name: "Chatty",
        pair: "",
        phase: "curve",
        price_change_24h_bps: 0,
        protocol_share_bps: 100,
        protocol_treasury: TOKEN,
        real_curve_eth: wad("5"),
        reserve_block: 10,
        reserve_hash: "0xabc",
        reserve_source: "indexed",
        spot_price_eth: wad("1"),
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
    () => ((window as unknown as { __sse?: SseHandle }).__sse?.open() ?? 0) > 0,
  );
  return (name: string, data: unknown) =>
    page.evaluate(
      ([eventName, payload]) =>
        (window as unknown as { __sse: SseHandle }).__sse.emit(eventName as string, payload),
      [name, data] as const,
    );
}

const list = (page: Page) => page.getByRole("list", { name: "Comments" });

test("a reader sees comments as plain text, loads more and gets live updates", async ({
  page,
}, testInfo) => {
  test.skip(testInfo.project.name !== "small-laptop", "One viewport is enough.");
  const state: State = {
    comments: [
      comment(3, "<script>window.__pwned = 1</script>", "alice"),
      comment(2, "second"),
      comment(1, "first"),
    ],
    firstPageCalls: [],
    walletStack: 0,
  };
  const emit = await openTokenPage(page, state);
  const panel = page.getByRole("region", { name: "Comments" });
  await expect(list(page).getByText("<script>window.__pwned = 1</script>")).toBeVisible();
  await expect(list(page).getByText("@alice")).toBeVisible();
  await expect(panel.getByText(/read-only|Sign in to comment/)).toBeVisible();
  // P0-C3: the public watch count shows beside the star.
  await expect(page.getByText("1,234 watching")).toBeVisible();
  expect(await page.evaluate(() => (window as unknown as { __pwned?: number }).__pwned)).toBe(
    undefined,
  );

  await panel.getByRole("button", { name: "Load more" }).click();
  await expect(list(page).getByText("first")).toBeVisible();
  const before = state.firstPageCalls.length;

  // A burst of hints for this token collapses into exactly one first-page refetch; a hint for
  // another token is ignored. The new comment shows while the older page stays loaded.
  state.comments = [comment(4, "fresh"), ...state.comments];
  const hint = { chain_id: CHAIN_ID, deployment_id: DEPLOYMENT_ID, token: TOKEN };
  await emit("comment", hint);
  await emit("comment", hint);
  await emit("comment", { ...hint, token: "0x00000000000000000000000000000000000000dd" });
  await expect(list(page).getByText("fresh")).toBeVisible();
  await expect(list(page).getByText("first")).toBeVisible();
  await page.waitForTimeout(800);
  expect(state.firstPageCalls.length - before).toBe(1);

  // A comment hidden by an operator disappears after the next hint.
  state.comments = state.comments.filter((c) => c.id !== "3");
  await emit("comment", hint);
  await expect(list(page).getByText("<script>window.__pwned = 1</script>")).toHaveCount(0);
  await expect(list(page).getByText("fresh")).toBeVisible();

  expect(state.walletStack).toBe(0);
});
