import { describe, expect, it } from "vitest";
import type { components } from "@/api/generated";
import { earlyBuyersView } from "./early-buyers";

type Body = components["schemas"]["EarlyBuyersBody"];
const supply = "1000000000000000000000000000"; // 1B tokens
const tokens = (whole: number) => `${BigInt(whole) * 10n ** 18n}`;
const snapshot = { chain_id: 1, as_of_block: 1, as_of_block_hash: "0x", finality: "safe" };

function body(overrides: Partial<Body> = {}): Body {
  return {
    snapshot,
    window_seconds: 60,
    window_open: false,
    early_wallet_count: 4,
    early_balance: tokens(200_000_000),
    cluster_count: 1,
    funding_checked: 0,
    clusters: [
      {
        wallet_count: 3,
        balance: tokens(150_000_000),
        links: ["same_transaction", "token_transfer"],
        includes_creator: true,
        wallets: [
          { address: "0x00000000000000000000000000000000000000aa", balance: tokens(100_000_000) },
          { address: "0x00000000000000000000000000000000000000bb", balance: tokens(50_000_000) },
        ],
      },
    ],
    ...overrides,
  };
}

describe("early buyers view", () => {
  it("summarises the early wallets and lists each linked group", () => {
    expect(earlyBuyersView(body(), supply)).toEqual({
      summary:
        "4 wallets got tokens from the curve in the first 60 seconds and hold 20.00% of the supply now.",
      groups: [
        {
          title: "3 wallets hold 15.00% of the supply",
          links: "received tokens in the same transaction and moved tokens between them",
          includesCreator: true,
          wallets: [
            { address: "0x00000000000000000000000000000000000000aa", balance: tokens(100_000_000) },
            { address: "0x00000000000000000000000000000000000000bb", balance: tokens(50_000_000) },
          ],
          unlisted: 1,
        },
      ],
      emptyGroups: null,
      moreGroups: 0,
      funding: null,
    });
  });

  it("states funding coverage only once some wallets were checked, and reads the funding link", () => {
    const view = earlyBuyersView(
      body({
        funding_checked: 3,
        clusters: [
          {
            wallet_count: 2,
            balance: "0",
            links: ["token_transfer", "funding_source"],
            includes_creator: false,
            wallets: null,
          },
        ],
      }),
      supply,
    );
    expect(view?.funding).toBe("Funding sources checked for 3 of 4 early wallets.");
    expect(view?.groups[0]?.links).toBe("moved tokens between them and funded by the same wallet");
  });

  it("says when no early wallets are linked, and when the window is still open", () => {
    const view = earlyBuyersView(
      body({ cluster_count: 0, clusters: [], early_wallet_count: 1, window_open: true }),
      supply,
    );
    expect(view?.summary).toBe(
      "1 wallet got tokens from the curve in the first 60 seconds and holds 20.00% of the supply now. The 60 seconds have not passed yet.",
    );
    expect(view?.groups).toEqual([]);
    expect(view?.emptyGroups).toBe("No linked early wallets found.");
  });

  it("handles a launch nobody bought into early", () => {
    expect(
      earlyBuyersView(
        body({ early_wallet_count: 0, early_balance: "0", cluster_count: 0, clusters: null }),
        supply,
      ),
    ).toEqual({
      summary: "No wallet got tokens from the curve in the first 60 seconds.",
      groups: [],
      emptyGroups: null,
      moreGroups: 0,
      funding: null,
    });
  });

  it("counts the groups beyond the listed ones and reads one link kind", () => {
    const view = earlyBuyersView(
      body({
        cluster_count: 12,
        clusters: [
          {
            wallet_count: 2,
            balance: "0",
            links: ["token_transfer"],
            includes_creator: false,
            wallets: null,
          },
        ],
      }),
      supply,
    );
    expect(view?.moreGroups).toBe(11);
    expect(view?.groups[0]).toMatchObject({
      title: "2 wallets hold 0.00% of the supply",
      links: "moved tokens between them",
      wallets: [],
      unlisted: 2,
    });
  });

  it("gives no view for a malformed amount or supply", () => {
    expect(earlyBuyersView(body({ early_balance: "1.5" }), supply)).toBeNull();
    expect(earlyBuyersView(body(), "0")).toBeNull();
    const bad = body();
    bad.clusters![0]!.balance = "-1";
    expect(earlyBuyersView(bad, supply)).toBeNull();
  });
});
