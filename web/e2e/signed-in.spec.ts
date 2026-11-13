import { expect, test, type Page, type Request } from "@playwright/test";

// Signed-in paths of P0-C2/C4: the comment composer, the username form and the inbox list. The
// fixture build mounts no Privy, so each test sets `window.__e2eSession` before load and the
// fixture-only session provider (src/e2e) feeds the same auth contexts Privy would. Every API
// call is mocked with page.route; the mocks assert the session's headers reach the API.
test.skip(
  !process.env.TASK6_ANVIL_API_URL,
  "Needs the fixture build (TASK6_ANVIL_* variables) so the configuration is ready.",
);
test.beforeEach(({}, testInfo) => {
  test.skip(testInfo.project.name !== "small-laptop", "One viewport is enough.");
});

const TOKEN = "0x00000000000000000000000000000000000000cc";
const WALLET_A = "0x00000000000000000000000000000000000000a1";
const WALLET_B = "0x00000000000000000000000000000000000000a2";
const SESSION = {
  userId: "did:privy:e2e-user",
  email: "reader@example.test",
  accessToken: "e2e-access-token",
  identityToken: "e2e-identity-token",
  linkedWallets: [WALLET_A, WALLET_B],
};
const snapshot = { chain_id: 31337, as_of_block: 10, as_of_block_hash: "0xabc", finality: "safe" };
const wad = (value: string) => `${BigInt(value) * 10n ** 18n}`;

type Json = (body: unknown, status?: number) => Promise<void>;
type Handler = (path: string, request: Request, json: Json) => Promise<void | false>;

async function signIn(page: Page) {
  await page.addInitScript((session) => {
    Object.assign(window, { __e2eSession: session });
    // Keep the live stream closed: these specs drive the REST paths only.
    class SilentEventSource extends EventTarget {
      readyState = 0;
      close() {
        this.readyState = 2;
      }
    }
    Object.assign(window, { EventSource: SilentEventSource });
  }, SESSION);
}

/** Routes `/v1/**` to `handler`; anything it leaves (returns false) gets an empty list. */
async function installApi(page: Page, handler: Handler) {
  const unauthenticated: string[] = [];
  await page.route("**/v1/**", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    const path = url.pathname.replace(/^.*\/v1/, "") + url.search;
    const json: Json = (body, status = 200) =>
      status === 204
        ? route.fulfill({ status })
        : route.fulfill({ status, contentType: "application/json", body: JSON.stringify(body) });
    if (path.startsWith("/me/") || request.method() !== "GET" || path.endsWith("/eligibility")) {
      const headers = request.headers();
      if (
        headers.authorization !== `Bearer ${SESSION.accessToken}` ||
        headers["privy-id-token"] !== SESSION.identityToken
      ) {
        unauthenticated.push(`${request.method()} ${path}`);
        return json({ type: "urn:launchpad:authentication_failed" }, 401);
      }
    }
    if ((await handler(path, request, json)) !== false) return;
    if (path.endsWith("/eth-usd") || path.endsWith("/image")) return json({}, 404);
    return json({ snapshot, items: [], next_cursor: undefined });
  });
  return unauthenticated;
}

const tokenDetail = {
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
};

test("a signed-in holder posts a comment as the chosen wallet", async ({ page }) => {
  const posts: unknown[] = [];
  let rateLimited = false;
  await signIn(page);
  const unauthenticated = await installApi(page, async (path, request, json) => {
    if (path === `/tokens/${TOKEN}/comments/eligibility`)
      return json({
        banned: false,
        username: "alice",
        eligible_wallets: [
          { address: WALLET_A, role: "holder" },
          { address: WALLET_B, role: "creator" },
        ],
      });
    if (path.startsWith(`/tokens/${TOKEN}/comments`) && request.method() === "POST") {
      const body = request.postDataJSON() as { body: string; wallet: string };
      if (rateLimited) return json({ type: "urn:launchpad:rate_limited" }, 429);
      posts.push(body);
      rateLimited = true;
      return json({
        id: "10",
        token: TOKEN,
        body: body.body,
        created_at: "2026-11-10T12:00:00.000000Z",
        author: { username: "alice", wallet: body.wallet, role: "creator" },
      });
    }
    if (path.startsWith(`/tokens/${TOKEN}/comments`))
      return json({ items: [], comment_count: 0, next_cursor: undefined });
    if (path.startsWith(`/tokens/${TOKEN}/watchers`)) return json({ watch_count: 0 });
    if (path.startsWith(`/tokens/${TOKEN}?`) || path === `/tokens/${TOKEN}`)
      return json(tokenDetail);
    return false;
  });

  await page.goto(`/token/${TOKEN}`);
  const panel = page.getByRole("region", { name: "Comments" });
  const composer = panel.getByRole("textbox", { name: "Comment" });
  await expect(composer).toBeVisible();
  await expect(panel.getByText("No comments yet.")).toBeVisible();

  // The body rules run client-side first: a blank body cannot be posted.
  const post = panel.getByRole("button", { name: "Post" });
  await composer.fill("   ");
  await expect(post).toBeDisabled();

  await panel.getByLabel("Post as").selectOption(WALLET_B);
  await composer.fill("  <b>gm</b> holders  ");
  await expect(panel.getByText(/^17\//)).toBeVisible();
  await post.click();

  const list = panel.getByRole("list", { name: "Comments" });
  await expect(list.getByText("<b>gm</b> holders")).toBeVisible();
  await expect(list.locator("b")).toHaveCount(0);
  await expect(list.getByText("@alice")).toBeVisible();
  await expect(list.getByText("Creator")).toBeVisible();
  await expect(composer).toHaveValue("");
  expect(posts).toEqual([{ body: "<b>gm</b> holders", wallet: WALLET_B }]);

  // A server rejection shows its message and keeps the draft.
  await composer.fill("again");
  await post.click();
  await expect(panel.getByRole("alert")).toHaveText("You are posting too fast. Try again shortly.");
  await expect(composer).toHaveValue("again");
  expect(unauthenticated).toEqual([]);
});

test("a holder-less account is told to hold the token instead of seeing a composer", async ({
  page,
}) => {
  await signIn(page);
  await installApi(page, async (path, _request, json) => {
    if (path === `/tokens/${TOKEN}/comments/eligibility`)
      return json({ banned: false, username: null, eligible_wallets: [] });
    if (path.startsWith(`/tokens/${TOKEN}/comments`))
      return json({ items: [], comment_count: 0, next_cursor: undefined });
    if (path.startsWith(`/tokens/${TOKEN}/watchers`)) return json({ watch_count: 0 });
    if (path.startsWith(`/tokens/${TOKEN}?`) || path === `/tokens/${TOKEN}`)
      return json(tokenDetail);
    return false;
  });
  await page.goto(`/token/${TOKEN}`);
  const panel = page.getByRole("region", { name: "Comments" });
  await expect(panel.getByText("Hold this token to comment.")).toBeVisible();
  await expect(panel.getByRole("textbox", { name: "Comment" })).toHaveCount(0);
});

test("a holder votes on a comment, switches and clears the vote, and sees refusals", async ({
  page,
}) => {
  const votes: Array<{ id: string; value: number }> = [];
  await signIn(page);
  const unauthenticated = await installApi(page, async (path, request, json) => {
    const vote = path.match(/^\/comments\/(\d+)\/vote$/);
    if (vote && request.method() === "PUT") {
      const { value } = request.postDataJSON() as { value: number };
      if (vote[1] === "6") return json({ type: "urn:launchpad:problem:not_holder" }, 403);
      votes.push({ id: vote[1]!, value });
      return json({ score: 3 + value, vote: value });
    }
    if (path === `/tokens/${TOKEN}/comments/eligibility`)
      return json({ banned: false, username: null, eligible_wallets: [] });
    if (path.startsWith(`/tokens/${TOKEN}/comments`))
      return json({
        comment_count: 2,
        next_cursor: undefined,
        items: ["5", "6"].map((id) => ({
          id,
          token: TOKEN,
          body: `comment ${id}`,
          created_at: "2026-11-10T12:00:00.000000Z",
          author: { username: null, wallet: WALLET_B, role: "holder" },
          score: 3,
        })),
      });
    if (path.startsWith(`/tokens/${TOKEN}/watchers`)) return json({ watch_count: 0 });
    if (path.startsWith(`/tokens/${TOKEN}?`) || path === `/tokens/${TOKEN}`)
      return json(tokenDetail);
    return false;
  });
  await page.goto(`/token/${TOKEN}`);
  const rows = page.getByRole("list", { name: "Comments" }).getByRole("listitem");
  const first = rows.filter({ hasText: "comment 5" });
  const up = first.getByRole("button", { name: "Upvote" });
  const down = first.getByRole("button", { name: "Downvote" });
  await expect(first.getByRole("group", { name: "Vote, 3 points" })).toBeVisible();

  await up.click();
  await expect(up).toHaveAttribute("aria-pressed", "true");
  await expect(first.getByRole("group", { name: "Vote, 4 points" })).toBeVisible();
  await first.screenshot({
    path: "test-results/screenshots/comment-votes.png",
    animations: "disabled",
  });
  await down.click();
  await expect(down).toHaveAttribute("aria-pressed", "true");
  await expect(up).toHaveAttribute("aria-pressed", "false");
  await expect(first.getByRole("group", { name: "Vote, 2 points" })).toBeVisible();
  await down.click();
  await expect(down).toHaveAttribute("aria-pressed", "false");
  await expect(first.getByRole("group", { name: "Vote, 3 points" })).toBeVisible();
  expect(votes).toEqual([
    { id: "5", value: 1 },
    { id: "5", value: -1 },
    { id: "5", value: 0 },
  ]);

  const second = rows.filter({ hasText: "comment 6" });
  await second.getByRole("button", { name: "Upvote" }).click();
  await expect(second.getByRole("alert")).toHaveText("Hold this token to vote on its comments.");
  await expect(second.getByRole("group", { name: "Vote, 3 points" })).toBeVisible();
  expect(unauthenticated).toEqual([]);
});

test("the username form saves a name and published wallets, and shows server rejections", async ({
  page,
}) => {
  const puts: unknown[] = [];
  let profile = {
    banned: false,
    username: null as string | null,
    username_changed_at: null as string | null,
    next_username_change_at: null as string | null,
    public_wallets: [] as string[],
  };
  await signIn(page);
  const unauthenticated = await installApi(page, async (path, request, json) => {
    if (path === "/me/public-profile" && request.method() === "GET") return json(profile);
    if (path === "/me/public-profile" && request.method() === "PUT") {
      const body = request.postDataJSON() as { username?: string; public_wallets: string[] };
      puts.push(body);
      if (body.username === "taken_name")
        return json({ type: "urn:launchpad:username_taken" }, 409);
      profile = {
        ...profile,
        username: body.username ?? profile.username,
        username_changed_at: "2026-11-10T12:00:00Z",
        next_username_change_at: new Date(Date.now() + 7 * 86_400_000).toISOString(),
        public_wallets: body.public_wallets,
      };
      return json(profile);
    }
    if (path.startsWith("/me/profile")) return json({ snapshot, items: [] });
    return false;
  });

  await page.goto("/profile");
  await expect(page.getByRole("heading", { name: "Your account", exact: true })).toBeVisible();
  await expect(page.getByText(SESSION.userId)).toBeVisible();
  const form = page.getByRole("region", { name: "Username" });
  const username = form.getByRole("textbox", { name: "Username" });
  await expect(username).toBeEnabled();

  // Client-side rule: an invalid name blocks Save before any request.
  await username.fill("No!");
  await expect(form.getByRole("button", { name: "Save" })).toBeDisabled();
  expect(puts).toEqual([]);

  await username.fill("taken_name");
  await form.getByRole("button", { name: "Save" }).click();
  await expect(form.getByRole("alert")).toHaveText("That username is taken.");

  await username.fill("alice_1");
  await form.getByRole("checkbox").first().check();
  await form.getByRole("button", { name: "Save" }).click();
  await expect(form.getByRole("status")).toHaveText("Saved.");
  await expect(form.getByText("@alice_1")).toBeVisible();
  expect(puts.at(-1)).toEqual({ username: "alice_1", public_wallets: [WALLET_A] });
  expect(unauthenticated).toEqual([]);
});

test("the inbox lists notifications, marks them read and loads more", async ({ page }) => {
  const reads: unknown[] = [];
  const item = (id: number, kind: "graduation" | "large_trade", symbol: string) => ({
    id: String(id),
    kind,
    token: TOKEN,
    name: symbol,
    symbol,
    eth_wei: wad("2"),
    side_buy: kind === "large_trade" ? true : null,
    source: kind === "large_trade" ? "curve" : null,
    trader: kind === "large_trade" ? WALLET_A : null,
    tx_hash: `0x${String(id).padStart(64, "0")}`,
    block_time: "2026-11-10T12:00:00Z",
  });
  await signIn(page);
  const unauthenticated = await installApi(page, async (path, request, json) => {
    if (path.startsWith("/me/notifications/read")) {
      reads.push(request.postDataJSON());
      return json(null, 204);
    }
    if (path.startsWith("/me/notifications")) {
      const cursor = new URLSearchParams(path.split("?")[1] ?? "").get("cursor");
      return cursor
        ? json({ items: [item(1, "large_trade", "OLD")], unread_count: 1 })
        : json({
            items: [item(3, "graduation", "<i>GRAD</i>"), item(2, "large_trade", "WHALE")],
            next_cursor: "older",
            unread_count: 1,
          });
    }
    return false;
  });

  await page.goto("/notifications");
  const list = page.getByRole("list").filter({ hasText: "graduated" });
  await expect(list.getByText("<i>GRAD</i> graduated with 2 ETH to the pool")).toBeVisible();
  await expect(list.locator("i")).toHaveCount(0);
  await expect(list.getByText("Large buy on WHALE: 2 ETH")).toBeVisible();
  // Only the unread_count newest items carry the New marker.
  await expect(list.getByText("New")).toHaveCount(1);
  await expect.poll(() => reads).toEqual([{ through: "3" }]);

  await page.getByRole("button", { name: "Load more" }).click();
  await expect(list.getByText("Large buy on OLD: 2 ETH")).toBeVisible();
  await expect(page.getByRole("button", { name: "Load more" })).toHaveCount(0);
  expect(unauthenticated).toEqual([]);
});

test("Telegram alerts connect through a deep link, save settings and disconnect", async ({
  page,
}) => {
  const puts: unknown[] = [];
  let linked = false;
  let settings = { graduations: true, large_trades: true, min_trade_wei: "500000000000000000" };
  const status = () => ({
    linked,
    linked_at: linked ? "2026-11-12T10:00:00.000000Z" : null,
    ...settings,
  });
  await signIn(page);
  await page.addInitScript(() => {
    // Record the deep link instead of opening a Telegram tab.
    Object.assign(window, {
      open: (url: string) => {
        Object.assign(window, { __openedLink: url });
        return null;
      },
    });
  });
  const unauthenticated = await installApi(page, async (path, request, json) => {
    if (path === "/me/telegram/link")
      return json({
        url: "https://t.me/LaunchBot?start=e2e-code",
        expires_at: "2026-11-12T10:15:00.000000Z",
      });
    if (path === "/me/telegram") {
      if (request.method() === "PUT") {
        const body = request.postDataJSON();
        puts.push(body);
        settings = body;
      }
      if (request.method() === "DELETE") {
        linked = false;
        return json(null, 204);
      }
      return json(status());
    }
    if (path.startsWith("/me/notifications")) return json({ items: [], unread_count: 0 });
    return false;
  });

  await page.goto("/notifications");
  const card = page.getByRole("region", { name: "Telegram alerts" });
  await card.getByRole("button", { name: "Connect Telegram" }).click();
  await expect(card.getByText("Waiting for Start in Telegram.")).toBeVisible();
  expect(
    await page.evaluate(() => (window as unknown as { __openedLink?: string }).__openedLink),
  ).toBe("https://t.me/LaunchBot?start=e2e-code");
  linked = true;
  await card.getByRole("button", { name: "I pressed Start" }).click();
  await expect(card.getByText("Connected. Alerts cover the tokens you watch.")).toBeVisible();

  const threshold = card.getByRole("textbox", { name: "Smallest trade in ETH" });
  await expect(threshold).toHaveValue("0.5");
  await threshold.fill("0.1");
  await card.getByRole("button", { name: "Save" }).click();
  await expect(card.getByRole("alert")).toHaveText("Enter a trade size from 0.5 to 10,000 ETH.");
  expect(puts).toEqual([]);
  await card.getByRole("checkbox", { name: "Graduations" }).uncheck();
  await threshold.fill("2.5");
  await card.getByRole("button", { name: "Save" }).click();
  await expect(card.getByRole("status")).toHaveText("Saved.");
  expect(puts).toEqual([
    { graduations: false, large_trades: true, min_trade_wei: "2500000000000000000" },
  ]);
  await page.screenshot({ path: "test-results/screenshots/telegram-alerts.png", fullPage: true });

  await card.getByRole("button", { name: "Disconnect" }).click();
  await expect(card.getByRole("button", { name: "Connect Telegram" })).toBeVisible();
  expect(unauthenticated).toEqual([]);
});

test("the Telegram card stays hidden when the API has no bot", async ({ page }) => {
  await signIn(page);
  await installApi(page, async (path, _request, json) => {
    if (path === "/me/telegram") return json({ type: "urn:launchpad:telegram_unavailable" }, 503);
    if (path.startsWith("/me/notifications")) return json({ items: [], unread_count: 0 });
    return false;
  });
  await page.goto("/notifications");
  await expect(page.getByRole("heading", { name: "No notifications yet" })).toBeVisible();
  await expect(page.getByRole("region", { name: "Telegram alerts" })).toHaveCount(0);
});
