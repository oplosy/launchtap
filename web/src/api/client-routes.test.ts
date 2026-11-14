import { describe, expect, it } from "vitest";
import {
  ApiClient,
  addWatchlist,
  getEthUsd,
  getHighlights,
  getProtocolDaily,
  getTokens,
  getWatchlist,
  removeWatchlist,
} from "./client";
import type { AuthHeaders } from "./response-types";

const base = "https://api.example";
const token = "0xAbC0000000000000000000000000000000000001";
const auth: AuthHeaders = { accessToken: "access-secret", identityToken: "identity-secret" };

type Sent = { url: string; method: string; headers: Headers; body: unknown };

function recordingFetch(sent: Sent[], response = () => new Response("{}", { status: 200 })) {
  return async (input: RequestInfo | URL, init?: RequestInit) => {
    sent.push({
      url: String(input),
      method: init?.method ?? "GET",
      headers: new Headers(init?.headers),
      body: init?.body,
    });
    return response();
  };
}

type Case = {
  name: string;
  call: (client: ApiClient) => Promise<unknown>;
  url: string;
  method?: string;
  body?: unknown;
  authed?: boolean;
  headers?: Record<string, string>;
};

const file = new File(["png"], "logo.png", { type: "image/png" });

// Every route the web app calls: path, method, body and whether it carries the caller's tokens.
const cases: Case[] = [
  { name: "profile", call: (c) => c.getProfile(auth), url: "/v1/profile", authed: true },
  {
    name: "token comments with paging",
    call: (c) => c.listTokenComments(token, { cursor: "c1", limit: 20 }),
    url: `/v1/tokens/${token}/comments?cursor=c1&limit=20`,
  },
  {
    name: "token comments without paging",
    call: (c) => c.listTokenComments(token),
    url: `/v1/tokens/${token}/comments`,
  },
  { name: "watchers", call: (c) => c.getTokenWatchers(token), url: `/v1/tokens/${token}/watchers` },
  {
    name: "comment eligibility",
    call: (c) => c.getCommentEligibility(token, auth),
    url: `/v1/tokens/${token}/comments/eligibility`,
    authed: true,
  },
  {
    name: "post comment",
    call: (c) => c.postTokenComment(token, { body: "gm", wallet: token }, auth),
    url: `/v1/tokens/${token}/comments`,
    method: "POST",
    body: { body: "gm", wallet: token },
    authed: true,
    headers: { "Content-Type": "application/json" },
  },
  {
    name: "vote comment",
    call: (c) => c.voteComment("id/1", -1, auth),
    url: "/v1/comments/id%2F1/vote",
    method: "PUT",
    body: { value: -1 },
    authed: true,
  },
  {
    name: "report comment",
    call: (c) => c.reportComment("id1", "spam", auth),
    url: "/v1/comments/id1/report",
    method: "PUT",
    body: { reason: "spam" },
    authed: true,
  },
  {
    name: "public profile",
    call: (c) => c.getPublicProfile(auth),
    url: "/v1/me/public-profile",
    authed: true,
  },
  {
    name: "save public profile",
    call: (c) => c.putPublicProfile({ username: "ann", public_wallets: [token] }, auth),
    url: "/v1/me/public-profile",
    method: "PUT",
    body: { username: "ann", public_wallets: [token] },
    authed: true,
  },
  {
    name: "delete public profile",
    call: (c) => c.deletePublicProfile(auth),
    url: "/v1/me/public-profile",
    method: "DELETE",
    authed: true,
  },
  { name: "watchlist", call: (c) => c.getWatchlist(auth), url: "/v1/me/watchlist", authed: true },
  {
    name: "watch",
    call: (c) => c.addWatchlist(token, auth),
    url: `/v1/me/watchlist/${token}`,
    method: "PUT",
    authed: true,
  },
  {
    name: "unwatch",
    call: (c) => c.removeWatchlist(token, auth),
    url: `/v1/me/watchlist/${token}`,
    method: "DELETE",
    authed: true,
  },
  {
    name: "forum best",
    call: (c) => c.getForumPosts({ sort: "best", cursor: "x", limit: 5 }),
    url: "/v1/forum/posts?sort=best&cursor=x&limit=5",
  },
  { name: "forum new", call: (c) => c.getForumPosts(), url: "/v1/forum/posts" },
  {
    name: "communities",
    call: (c) => c.getForumCommunities("hot", 10),
    url: "/v1/forum/communities?sort=hot&limit=10",
  },
  {
    name: "communities without limit",
    call: (c) => c.getForumCommunities("top"),
    url: "/v1/forum/communities?sort=top",
  },
  {
    name: "notifications",
    call: (c) => c.getNotifications(auth, { cursor: "n", limit: 3 }),
    url: "/v1/me/notifications?cursor=n&limit=3",
    authed: true,
  },
  {
    name: "notifications first page",
    call: (c) => c.getNotifications(auth),
    url: "/v1/me/notifications",
    authed: true,
  },
  {
    name: "mark notifications read",
    call: (c) => c.markNotificationsRead("n9", auth),
    url: "/v1/me/notifications/read",
    method: "PUT",
    body: { through: "n9" },
    authed: true,
  },
  { name: "telegram", call: (c) => c.getTelegram(auth), url: "/v1/me/telegram", authed: true },
  {
    name: "telegram settings",
    call: (c) => c.updateTelegram({ trades: true } as never, auth),
    url: "/v1/me/telegram",
    method: "PUT",
    body: { trades: true },
    authed: true,
  },
  {
    name: "telegram unlink",
    call: (c) => c.unlinkTelegram(auth),
    url: "/v1/me/telegram",
    method: "DELETE",
    authed: true,
  },
  {
    name: "telegram link",
    call: (c) => c.createTelegramLink(auth),
    url: "/v1/me/telegram/link",
    method: "POST",
    authed: true,
  },
  {
    name: "metadata write",
    call: (c) => c.updateTokenMetadata(token, { description: "d" } as never, 3, '"etag-3"', auth),
    url: `/v1/tokens/${token}/metadata`,
    method: "PUT",
    body: { description: "d" },
    authed: true,
    headers: { "If-Match": '"etag-3"' },
  },
  {
    name: "metadata write without etag",
    call: (c) => c.updateTokenMetadata(token, { description: "d" } as never, 4, null, auth),
    url: `/v1/tokens/${token}/metadata`,
    method: "PUT",
    body: { description: "d" },
    authed: true,
    headers: { "If-Match": '"4"' },
  },
  {
    name: "metadata read",
    call: (c) => c.getTokenMetadata(token),
    url: `/v1/tokens/${token}/metadata`,
  },
  {
    name: "image write",
    call: (c) => c.updateTokenImage(token, file, 7, auth),
    url: `/v1/tokens/${token}/image`,
    method: "PUT",
    authed: true,
    headers: { "If-Match": '"7"', "Content-Type": "image/png" },
  },
  {
    name: "protocol daily",
    call: (c) => c.getProtocolDaily({ from: "2026-11-01", limit: 7 }),
    url: "/v1/stats/protocol/daily?from=2026-11-01&limit=7",
  },
  {
    name: "protocol daily all",
    call: (c) => c.getProtocolDaily(),
    url: "/v1/stats/protocol/daily",
  },
  { name: "protocol", call: (c) => c.getProtocol(), url: "/v1/stats/protocol" },
  { name: "revenue", call: (c) => c.getProtocolRevenue(), url: "/v1/stats/protocol/revenue" },
  { name: "eth usd", call: (c) => c.getEthUsd(), url: "/v1/reference/eth-usd" },
  { name: "recent trades", call: (c) => c.listRecentTrades(), url: "/v1/trades/recent?limit=50" },
  { name: "highlights", call: (c) => c.getHighlights(), url: "/v1/tokens/highlights" },
  { name: "token", call: (c) => c.getToken(token), url: `/v1/tokens/${token}` },
  {
    name: "candles",
    call: (c) => c.getCandles(token, { interval: "1h", from: "", limit: 100 }),
    url: `/v1/tokens/${token}/candles?interval=1h&limit=100`,
  },
  { name: "candles default", call: (c) => c.getCandles(token), url: `/v1/tokens/${token}/candles` },
  {
    name: "trades",
    call: (c) => c.getTrades(token, { cursor: "t", limit: 25 }),
    url: `/v1/tokens/${token}/trades?cursor=t&limit=25`,
  },
  { name: "holders", call: (c) => c.getHolders(token), url: `/v1/tokens/${token}/holders` },
  {
    name: "early buyers",
    call: (c) => c.getEarlyBuyers(token),
    url: `/v1/tokens/${token}/early-buyers`,
  },
  { name: "wallet", call: (c) => c.getWallet(token), url: `/v1/wallets/${token}` },
  {
    name: "closed positions",
    call: (c) => c.listWalletPositions(token, { closed: true, cursor: "p" }),
    url: `/v1/wallets/${token}/positions?closed=true&cursor=p`,
  },
  {
    name: "wallet trades",
    call: (c) => c.listWalletTrades(token, { limit: 10 }),
    url: `/v1/wallets/${token}/trades?limit=10`,
  },
  {
    name: "wallet created",
    call: (c) => c.listWalletCreated(token),
    url: `/v1/wallets/${token}/created`,
  },
  { name: "leaderboard", call: (c) => c.getLeaderboard(20), url: "/v1/leaderboard?limit=20" },
  {
    name: "quote",
    call: (c) => c.getQuote(token, { side: "buy", amount: "1" } as never),
    url: `/v1/tokens/${token}/quote`,
    method: "POST",
    body: { side: "buy", amount: "1" },
  },
  {
    name: "canonical transaction",
    call: (c) => c.getCanonicalTransaction("0xhash"),
    url: "/v1/transactions/0xhash",
  },
];

describe("API client routes", () => {
  it.each(cases)("$name", async ({ call, url, method, body, authed, headers }) => {
    const sent: Sent[] = [];
    await call(new ApiClient({ baseUrl: base, fetch: recordingFetch(sent) }));
    expect(sent).toHaveLength(1);
    const request = sent[0];
    if (!request) throw new Error("no request sent");
    expect(request.url).toBe(base + url);
    expect(request.method).toBe(method ?? "GET");
    if (body === undefined) {
      if (!(request.body instanceof File)) expect(request.body).toBeUndefined();
    } else {
      expect(JSON.parse(String(request.body))).toEqual(body);
    }
    expect(request.headers.get("Authorization")).toBe(authed ? "Bearer access-secret" : null);
    expect(request.headers.get("privy-id-token")).toBe(authed ? "identity-secret" : null);
    for (const [name, value] of Object.entries(headers ?? {})) {
      expect(request.headers.get(name)).toBe(value);
    }
    // Tokens travel only in headers, never in the URL.
    expect(request.url).not.toContain("secret");
  });

  it("reads the image revision and ETag, falling back to 0 for a malformed revision", async () => {
    const respond = (revision: string) =>
      new ApiClient({
        baseUrl: base,
        fetch: async () =>
          new Response("png", { status: 200, headers: { "X-Revision": revision, ETag: '"h"' } }),
      }).getTokenImageRevision(token);
    await expect(respond("12")).resolves.toEqual({ revision: 12, etag: '"h"' });
    await expect(respond("-1")).resolves.toEqual({ revision: 0, etag: '"h"' });
    await expect(respond("1.5")).resolves.toEqual({ revision: 0, etag: '"h"' });
    const sent: Sent[] = [];
    await new ApiClient({ baseUrl: base, fetch: recordingFetch(sent) }).getTokenImageRevision(
      token,
    );
    expect(sent[0]?.headers.get("Accept")).toBe("image/*");
  });

  it("returns the body and ETag of a revisioned write", async () => {
    const client = new ApiClient({
      baseUrl: base,
      fetch: async () =>
        new Response(JSON.stringify({ revision: 4 }), { status: 200, headers: { ETag: '"4"' } }),
    });
    await expect(client.getTokenMetadata(token)).resolves.toEqual({
      body: { revision: 4 },
      etag: '"4"',
    });
  });

  it("maps a failed no-content write to its problem", async () => {
    const client = new ApiClient({
      baseUrl: base,
      fetch: async () => new Response("not json", { status: 503 }),
    });
    await expect(client.addWatchlist(token, auth)).rejects.toMatchObject({ status: 503 });
  });

  it("module helpers build a client for the given base URL", async () => {
    const sent: string[] = [];
    const original = globalThis.fetch;
    globalThis.fetch = (async (input: RequestInfo | URL) => {
      sent.push(String(input));
      return new Response("{}", { status: 200 });
    }) as typeof fetch;
    try {
      await getProtocolDaily(base, { limit: 1 });
      await getTokens(base, { q: "x" });
      await getEthUsd(base);
      await getHighlights(base);
      await getWatchlist(base, auth);
      await addWatchlist(base, token, auth);
      await removeWatchlist(base, token, auth);
    } finally {
      globalThis.fetch = original;
    }
    expect(sent).toEqual([
      `${base}/v1/stats/protocol/daily?limit=1`,
      `${base}/v1/tokens?q=x`,
      `${base}/v1/reference/eth-usd`,
      `${base}/v1/tokens/highlights`,
      `${base}/v1/me/watchlist`,
      `${base}/v1/me/watchlist/${token}`,
      `${base}/v1/me/watchlist/${token}`,
    ]);
  });
});
