import { describe, expect, it } from "vitest";
import { marketLinks } from "./market-links";

const PAIR = "0xABCDEFabcdefABCDEFabcdefABCDEFabcdefABCD";
const both = { dexscreenerChain: "robinhood", geckoterminalNetwork: "robinhood-chain" };

describe("marketLinks", () => {
  it("builds both URLs with a lowercased pair", () => {
    expect(marketLinks({ marketLinks: both }, PAIR, "graduated")).toEqual({
      dexscreener: "https://dexscreener.com/robinhood/0xabcdefabcdefabcdefabcdefabcdefabcdefabcd",
      geckoterminal:
        "https://www.geckoterminal.com/robinhood-chain/pools/0xabcdefabcdefabcdefabcdefabcdefabcdefabcd",
    });
  });
  it("returns only the link whose identifier is present", () => {
    expect(
      Object.keys(marketLinks({ marketLinks: { geckoterminalNetwork: "x" } }, PAIR, "graduated")),
    ).toEqual(["geckoterminal"]);
    expect(
      Object.keys(marketLinks({ marketLinks: { dexscreenerChain: "x" } }, PAIR, "graduated")),
    ).toEqual(["dexscreener"]);
  });
  it("returns nothing without market links or deployment", () => {
    expect(marketLinks({ marketLinks: null }, PAIR, "graduated")).toEqual({});
    expect(marketLinks(null, PAIR, "graduated")).toEqual({});
  });
  it("returns nothing on the curve phase", () => {
    expect(marketLinks({ marketLinks: both }, PAIR, "curve")).toEqual({});
  });
  it("returns nothing for a malformed or missing pair", () => {
    expect(marketLinks({ marketLinks: both }, "0x123", "graduated")).toEqual({});
    expect(marketLinks({ marketLinks: both }, null, "graduated")).toEqual({});
  });
  it("rejects unsafe identifiers", () => {
    expect(
      marketLinks(
        { marketLinks: { dexscreenerChain: "a/../b", geckoterminalNetwork: "x?y" } },
        PAIR,
        "graduated",
      ),
    ).toEqual({});
  });
});
