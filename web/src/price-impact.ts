// How far a curve trade's own price is from the last indexed spot price, fees excluded (they
// are listed separately). Buy fees come off the ETH in and sell fees off the ETH out, as in
// CurveMath.quoteBuy / quoteSell, so a buy is measured on its net ETH and a sell on its gross ETH.
// Exact bigint math, rounded up, so a warning never understates the impact.

const WAD = 10n ** 18n;
const BPS = 10_000n;
/** At or above this impact the trade panel warns before review. */
export const HIGH_PRICE_IMPACT_BPS = 500n;

export type PriceImpact = { bps: bigint; percent: string; high: boolean };

export function curvePriceImpact(trade: {
  side: "buy" | "sell";
  input: bigint;
  output: bigint;
  fee: bigint;
  spotPriceWad: bigint | null;
}): PriceImpact | null {
  const { side, input, output, fee, spotPriceWad } = trade;
  if (spotPriceWad === null || spotPriceWad <= 0n) return null;
  if (input <= 0n || output < 0n || fee < 0n) return null;

  // expected: what the trade is worth at spot; received: what the curve actually gives, in the
  // same units (wei x wad).
  let expected: bigint;
  let received: bigint;
  if (side === "buy") {
    const net = input - fee;
    if (net <= 0n) return null;
    expected = net * WAD;
    received = output * spotPriceWad;
  } else {
    expected = input * spotPriceWad;
    received = (output + fee) * WAD;
  }
  const deficit = expected > received ? expected - received : 0n;
  const bps = (deficit * BPS + expected - 1n) / expected;
  const fraction = (bps % 100n).toString().padStart(2, "0");
  return { bps, percent: `${bps / 100n}.${fraction}%`, high: bps >= HIGH_PRICE_IMPACT_BPS };
}
