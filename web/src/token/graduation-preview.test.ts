import { describe, expect, it } from "vitest";
import { graduationPreview, type GraduationPreviewInput } from "./graduation-preview";

const WAD = 10n ** 18n;

const v1: GraduationPreviewInput = {
  phase: "curve",
  graduation_eth: ((42n * WAD) / 10n).toString(),
  real_curve_eth: ((168n * WAD) / 100n).toString(),
  lp_tokens: (200_000_000n * WAD).toString(),
  total_supply: (1_000_000_000n * WAD).toString(),
};

describe("graduationPreview", () => {
  it("derives remaining ETH, opening price and opening market cap for v1", () => {
    expect(graduationPreview(v1)).toEqual({
      remainingWei: ((252n * WAD) / 100n).toString(),
      // 4.2 ETH / 200M tokens = 2.1e-8 ETH per whole token, in wad.
      openingPriceWad: "21000000000",
      // 2.1e-8 ETH × 1B tokens = 21 ETH.
      openingMarketCapWei: (21n * WAD).toString(),
    });
  });

  it("floors the price like integer pool math instead of rounding up", () => {
    const preview = graduationPreview({
      ...v1,
      lp_tokens: (3n * WAD).toString(),
      graduation_eth: "10",
    });
    expect(preview?.openingPriceWad).toBe("3");
  });

  it("clamps remaining ETH at zero once the target is reached", () => {
    expect(graduationPreview({ ...v1, real_curve_eth: (5n * WAD).toString() })?.remainingWei).toBe(
      "0",
    );
  });

  it("is hidden once the token has graduated", () => {
    expect(graduationPreview({ ...v1, phase: "graduated" })).toBeNull();
  });

  it("returns unavailable legs for malformed or zero inputs", () => {
    expect(
      graduationPreview({ ...v1, lp_tokens: "0", real_curve_eth: "x", total_supply: "1.5" }),
    ).toEqual({ remainingWei: null, openingPriceWad: null, openingMarketCapWei: null });
    expect(graduationPreview({ ...v1, graduation_eth: "-1" })).toEqual({
      remainingWei: null,
      openingPriceWad: null,
      openingMarketCapWei: null,
    });
  });
});
