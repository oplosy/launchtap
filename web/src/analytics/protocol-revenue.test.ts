import { describe, expect, it } from "vitest";
import type { components } from "@/api/generated";
import { protocolRevenueView } from "./protocol-revenue";

type Body = components["schemas"]["RevenueBody"];
const eth = (whole: number) => `${BigInt(whole) * 10n ** 18n}`;
const source = (engine1: string, engine2: string, total: string) => ({
  engine_1: engine1,
  engine_2: engine2,
  total,
});
const zero = source("0", "0", "0");

function body(overrides: Partial<Body["sources"]> = {}, total = eth(6)): Body {
  return {
    snapshot: { chain_id: 1, as_of_block: 9, as_of_block_hash: "0x", finality: "safe" },
    total,
    sources: {
      launch_fees: source(eth(1), eth(1), eth(2)),
      trade_fees: source(eth(1), eth(2), eth(3)),
      snipe_tax: zero,
      graduation_remainder: zero,
      lp_fees: source("0", eth(1), eth(1)),
      ...overrides,
    },
  };
}

describe("protocol revenue view", () => {
  it("lists the sources that earned anything, in a fixed order, with engine splits", () => {
    expect(protocolRevenueView(body())).toEqual({
      total: "6 ETH",
      rows: [
        { label: "Launch fees", engine1: "1 ETH", engine2: "1 ETH", total: "2 ETH" },
        { label: "Trade fees", engine1: "1 ETH", engine2: "2 ETH", total: "3 ETH" },
        { label: "LP fees", engine1: "0 ETH", engine2: "1 ETH", total: "1 ETH" },
      ],
      block: 9,
    });
  });

  it("names every source when all have earned", () => {
    const view = protocolRevenueView(
      body({ snipe_tax: source("0", "5", "5"), graduation_remainder: source("0", "7", "7") }),
    );
    expect(view?.rows.map((row) => row.label)).toEqual([
      "Launch fees",
      "Trade fees",
      "Snipe tax",
      "Graduation remainder",
      "LP fees",
    ]);
  });

  it("has no rows before any revenue", () => {
    expect(
      protocolRevenueView(body({ launch_fees: zero, trade_fees: zero, lp_fees: zero }, "0")),
    ).toEqual({ total: "0 ETH", rows: [], block: 9 });
  });

  it("gives no view for a malformed amount", () => {
    expect(protocolRevenueView(body({}, "1.5"))).toBeNull();
    expect(protocolRevenueView(body({ trade_fees: source("-1", "0", "0") }))).toBeNull();
  });
});
