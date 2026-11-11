import type { components } from "@/api/generated";
import { formatCanonicalBaseUnits, parseCanonicalBaseUnits } from "@/amounts";

export type LaunchFactsInput = Pick<
  components["schemas"]["TokenDetailDTO"],
  | "engine_version"
  | "total_supply"
  | "curve_tokens"
  | "lp_tokens"
  | "trade_fee_bps"
  | "protocol_share_bps"
  | "graduation_eth"
  | "phase"
>;

export type SupplyShare = { amount: string | null; percent: string | null };

/** Display-ready launch facts; every `null` renders as "Unavailable", never as a guess. */
export type LaunchFacts = {
  engineVersion: string | null;
  totalSupply: string | null;
  curveShare: SupplyShare;
  lpShare: SupplyShare;
  tradeFee: string | null;
  protocolFee: string | null;
  creatorFee: string | null;
  graduationEth: string | null;
  graduated: boolean;
  transferRule: string;
  pauseRule: string;
};

const BPS = 10_000n;
/** trade_fee_bps × protocol_share_bps is in units of 1e-6 percent. */
const MICRO_PERCENT_DIGITS = 6;

function canonical(value: string): bigint | null {
  try {
    return parseCanonicalBaseUnits(value);
  } catch {
    return null;
  }
}

function bps(value: number): bigint | null {
  return Number.isInteger(value) && value >= 0 && value <= 10_000 ? BigInt(value) : null;
}

/** Fixed-point percent with at least two decimals, trailing zeros beyond that trimmed. */
function percent(value: bigint, digits: number): string {
  const scale = 10n ** BigInt(digits);
  const whole = value / scale;
  const fraction = (value % scale)
    .toString()
    .padStart(digits, "0")
    .replace(/0+$/, "")
    .padEnd(2, "0");
  return `${whole}.${fraction}%`;
}

function share(part: bigint | null, total: bigint | null): SupplyShare {
  const amount = part === null ? null : formatCanonicalBaseUnits(part.toString(), 4);
  if (part === null || total === null || total === 0n || part > total) {
    return { amount, percent: null };
  }
  return { amount, percent: percent((part * BPS * 100n) / total, 4) };
}

/**
 * Launch economics as stored onchain for a v1 token. The privilege statements mirror
 * `LaunchToken._update` (curve-phase transfer guard) and `BondingCurveV1._requireTradingAllowed`
 * (factory trading pause applies to curve buys and sells only).
 */
export function launchFacts(token: LaunchFactsInput): LaunchFacts {
  const total = canonical(token.total_supply);
  const tradeBps = bps(token.trade_fee_bps);
  const shareBps = bps(token.protocol_share_bps);
  const fees =
    tradeBps === null || shareBps === null
      ? null
      : { trade: tradeBps * BPS, protocol: tradeBps * shareBps };
  const graduated = token.phase === "graduated";
  const graduationWei = canonical(token.graduation_eth);

  return {
    engineVersion:
      Number.isInteger(token.engine_version) && token.engine_version > 0
        ? `v${token.engine_version}`
        : null,
    totalSupply: total === null ? null : formatCanonicalBaseUnits(total.toString(), 4),
    curveShare: share(canonical(token.curve_tokens), total),
    lpShare: share(canonical(token.lp_tokens), total),
    tradeFee: fees === null ? null : percent(fees.trade, MICRO_PERCENT_DIGITS),
    protocolFee: fees === null ? null : percent(fees.protocol, MICRO_PERCENT_DIGITS),
    creatorFee: fees === null ? null : percent(fees.trade - fees.protocol, MICRO_PERCENT_DIGITS),
    graduationEth:
      graduationWei === null ? null : formatCanonicalBaseUnits(graduationWei.toString(), 6),
    graduated,
    transferRule: graduated
      ? "No transfer restrictions. The token has no owner, mint, tax or blacklist."
      : "Until graduation, only the curve can move tokens into or out of the curve or the pair. The token has no owner, mint, tax or blacklist.",
    pauseRule: graduated
      ? "The protocol cannot pause trading on the Uniswap V2 pair."
      : "The protocol pause authority can pause curve buys and sells until graduation.",
  };
}
