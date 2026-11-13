import { describe, expect, it } from "vitest";
import { launchBlockHolders, topHolderShare } from "./holder-concentration";

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

describe("launch block holders", () => {
  const holder = (whole: number, block: number) => ({
    balance: tokens(whole),
    first_acquired_block: block,
  });

  it("sums the holders who first got in at the launch block", () => {
    expect(
      launchBlockHolders(
        [holder(50_000_000, 100), holder(30_000_000, 101), holder(25_000_000, 100)],
        100,
        supply,
        false,
      ),
    ).toBe(
      "2 holders first got in at the launch block (bundled with the launch, including any launch buy by the creator) and still hold 7.50% of the supply.",
    );
  });

  it("uses the singular and notes a partial list", () => {
    expect(launchBlockHolders([holder(10_000_000, 100)], 100, supply, true)).toBe(
      "1 holder first got in at the launch block (bundled with the launch, including any launch buy by the creator) and still holds 1.00% of the supply, among the holders loaded so far.",
    );
  });

  it("says when no current holder got in at the launch block", () => {
    expect(launchBlockHolders([holder(1, 101)], 100, supply, false)).toBe(
      "No current holder first got in at the launch block.",
    );
    expect(launchBlockHolders([holder(1, 101)], 100, supply, true)).toBe(
      "No holder loaded so far first got in at the launch block.",
    );
  });

  it("gives no line without a usable launch block, supply or balance", () => {
    expect(launchBlockHolders([holder(1, 100)], undefined, supply, false)).toBeNull();
    expect(launchBlockHolders([holder(1, 100)], -1, supply, false)).toBeNull();
    expect(launchBlockHolders([holder(1, 100)], 100, "0", false)).toBeNull();
    expect(
      launchBlockHolders([{ balance: "1.5", first_acquired_block: 100 }], 100, supply, false),
    ).toBeNull();
    expect(launchBlockHolders([], 100, supply, false)).toBeNull();
  });
});
