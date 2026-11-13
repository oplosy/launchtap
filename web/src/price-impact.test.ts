import { describe, expect, it } from "vitest";
import { curvePriceImpact } from "./price-impact";

const WAD = 10n ** 18n;

describe("curve price impact", () => {
  it("compares a buy's net ETH with the tokens it would get at the spot price", () => {
    // Spot 2 ETH per token: 10 ETH in, 0.1 ETH fee, 9.9 ETH net would buy 4.95 tokens at spot.
    // The curve gives 4.5, so the trade moves the price by 1 - 4.5 / 4.95 = 9.0909%, shown rounded up as 9.10%.
    expect(
      curvePriceImpact({
        side: "buy",
        input: 10n * WAD,
        output: 45n * (WAD / 10n),
        fee: WAD / 10n,
        spotPriceWad: 2n * WAD,
      }),
    ).toEqual({ bps: 910n, percent: "9.10%", high: true });
  });

  it("compares a sell's gross ETH with the input's value at the spot price", () => {
    // Spot 2 ETH: 10 tokens are worth 20 ETH. The curve pays 19.5 gross (19.4 out + 0.1 fee).
    expect(
      curvePriceImpact({
        side: "sell",
        input: 10n * WAD,
        output: 194n * (WAD / 10n),
        fee: WAD / 10n,
        spotPriceWad: 2n * WAD,
      }),
    ).toEqual({ bps: 250n, percent: "2.50%", high: false });
  });

  it("rounds up, so a small impact is never shown as zero", () => {
    // 1 - 999_999 / 1_000_000 = 0.0001% rounds up to 0.01%.
    expect(
      curvePriceImpact({
        side: "buy",
        input: 1_000_000n,
        output: 999_999n,
        fee: 0n,
        spotPriceWad: WAD,
      }),
    ).toEqual({ bps: 1n, percent: "0.01%", high: false });
  });

  it("reads a better-than-spot fill (a stale spot price) as no impact", () => {
    expect(
      curvePriceImpact({ side: "buy", input: WAD, output: 2n * WAD, fee: 0n, spotPriceWad: WAD }),
    ).toEqual({ bps: 0n, percent: "0.00%", high: false });
  });

  it("gives no figure without a usable spot price or trade", () => {
    const trade = { side: "buy" as const, input: WAD, output: WAD, fee: 0n };
    expect(curvePriceImpact({ ...trade, spotPriceWad: 0n })).toBeNull();
    expect(curvePriceImpact({ ...trade, spotPriceWad: null })).toBeNull();
    expect(curvePriceImpact({ ...trade, input: 0n, spotPriceWad: WAD })).toBeNull();
    // A buy whose fee eats the whole input has nothing to compare.
    expect(curvePriceImpact({ ...trade, fee: WAD, spotPriceWad: WAD })).toBeNull();
  });
});
