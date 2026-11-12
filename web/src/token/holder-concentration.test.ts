import { describe, expect, it } from "vitest";
import { topHolderShare } from "./holder-concentration";

const supply = "1000000000000000000000000000"; // 1B tokens
const tokens = (whole: number) => `${BigInt(whole) * 10n ** 18n}`;

describe("top holder share", () => {
  it("sums the ten largest balances over the whole supply, rounded down to 0.01%", () => {
    // Twelve holders of 10M each (the list arrives largest first): the top ten hold 100M = 10%.
    const balances = Array.from({ length: 12 }, () => tokens(10_000_000));
    expect(topHolderShare(balances, supply)).toEqual({ holders: 10, percent: "10.00%" });
    // 1/3 of the supply rounds down, never up.
    expect(topHolderShare(["333333333333333333333333333"], supply)).toEqual({
      holders: 1,
      percent: "33.33%",
    });
  });

  it("uses every holder when fewer than ten exist", () => {
    expect(topHolderShare([tokens(50_000_000), tokens(25_000_000)], supply)).toEqual({
      holders: 2,
      percent: "7.50%",
    });
  });

  it("gives no figure for an empty list, a zero supply or a malformed amount", () => {
    expect(topHolderShare([], supply)).toBeNull();
    expect(topHolderShare([tokens(1)], "0")).toBeNull();
    expect(topHolderShare(["1.5"], supply)).toBeNull();
    expect(topHolderShare([tokens(1)], "-1")).toBeNull();
  });
});
