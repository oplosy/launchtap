import type { components } from "@/api/generated";
import { parseCanonicalBaseUnits } from "@/amounts";

export type GraduationPreviewInput = Pick<
  components["schemas"]["TokenDetailDTO"],
  "phase" | "graduation_eth" | "real_curve_eth" | "lp_tokens" | "total_supply"
>;

/** Canonical base-unit strings; `null` legs render as "Unavailable". */
export type GraduationPreview = {
  remainingWei: string | null;
  /** ETH per whole token, 18-decimal wad. */
  openingPriceWad: string | null;
  openingMarketCapWei: string | null;
};

const WAD = 10n ** 18n;

function canonical(value: string): bigint | null {
  try {
    return parseCanonicalBaseUnits(value);
  } catch {
    return null;
  }
}

/**
 * What the pool opens at when the curve graduates. Mirrors `BondingCurveV1._graduate`, which
 * deposits exactly `graduationEth` as WETH and `lpTokens` of the token into the V2 pair, so the
 * implied opening price is their ratio. Returns `null` after graduation.
 */
export function graduationPreview(token: GraduationPreviewInput): GraduationPreview | null {
  if (token.phase === "graduated") return null;
  const target = canonical(token.graduation_eth);
  const raised = canonical(token.real_curve_eth);
  const lpTokens = canonical(token.lp_tokens);
  const supply = canonical(token.total_supply);
  const priced = target !== null && lpTokens !== null && lpTokens > 0n;

  return {
    remainingWei:
      target === null || raised === null
        ? null
        : (target > raised ? target - raised : 0n).toString(),
    openingPriceWad: priced ? ((target * WAD) / lpTokens).toString() : null,
    openingMarketCapWei:
      priced && supply !== null ? ((target * supply) / lpTokens).toString() : null,
  };
}
