import { formatBps } from "@/launch-v2";

// Engine-1 launch facts for the token page: what the contracts guarantee by construction and
// the pair the curve seeds at graduation. Every number comes from the indexed launch
// parameters, so the page states only what the chain already fixed at launch.

export type LaunchFacts = {
  tradeFee: string;
  feeSplit: string;
  graduation: {
    /** Wei per whole token of the seeded pair (mirrors BondingCurveV1._graduate). */
    openingPriceWad: string;
    /** The opening price over the whole fixed supply, in wei. */
    marketCapWei: string;
    /** ETH still to raise before graduation, in wei; null once graduated. */
    remainingEthWei: string | null;
  };
};

const CANONICAL = /^(0|[1-9][0-9]*)$/;
const BPS_DENOMINATOR = 10_000;

function canonical(value: string): bigint | null {
  return CANONICAL.test(value) ? BigInt(value) : null;
}

function validBps(value: number) {
  return Number.isInteger(value) && value >= 0 && value <= BPS_DENOMINATOR;
}

export function launchFacts(token: {
  engine_version: number;
  phase: string;
  total_supply: string;
  lp_tokens: string;
  graduation_eth: string;
  real_curve_eth: string;
  trade_fee_bps: number;
  protocol_share_bps: number;
}): LaunchFacts | null {
  // Engine 2 graduates into a V4 pool on a different seeding rule; it has its own facts panel.
  if (token.engine_version !== 1) return null;
  const supply = canonical(token.total_supply);
  const lpTokens = canonical(token.lp_tokens);
  const graduationEth = canonical(token.graduation_eth);
  const raised = canonical(token.real_curve_eth);
  if (supply === null || lpTokens === null || graduationEth === null || raised === null)
    return null;
  if (lpTokens === 0n || !validBps(token.trade_fee_bps) || !validBps(token.protocol_share_bps))
    return null;

  const graduated = token.phase.toLowerCase() === "graduated";
  const remaining = graduationEth > raised ? graduationEth - raised : 0n;
  return {
    tradeFee: formatBps(token.trade_fee_bps),
    feeSplit: `${formatBps(token.protocol_share_bps)} to the protocol, ${formatBps(
      BPS_DENOMINATOR - token.protocol_share_bps,
    )} to the creator`,
    graduation: {
      openingPriceWad: ((graduationEth * 10n ** 18n) / lpTokens).toString(),
      marketCapWei: ((graduationEth * supply) / lpTokens).toString(),
      remainingEthWei: graduated ? null : remaining.toString(),
    },
  };
}
