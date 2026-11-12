// Share of the whole supply held by the largest holders, from the holders list the API already
// returns largest first with the curve, pool, locker and burn addresses excluded. Exact bigint
// math, rounded down to 0.01%, so the page never overstates or invents concentration.

const CANONICAL = /^(0|[1-9][0-9]*)$/;

export function topHolderShare(
  balances: readonly string[],
  totalSupply: string,
  count = 10,
): { holders: number; percent: string } | null {
  if (!balances.length || !CANONICAL.test(totalSupply)) return null;
  const supply = BigInt(totalSupply);
  if (supply === 0n) return null;
  const top = balances.slice(0, count);
  if (!top.every((balance) => CANONICAL.test(balance))) return null;
  const held = top.reduce((sum, balance) => sum + BigInt(balance), 0n);
  const hundredths = (held * 10_000n) / supply;
  const whole = hundredths / 100n;
  const fraction = (hundredths % 100n).toString().padStart(2, "0");
  return { holders: top.length, percent: `${whole}.${fraction}%` };
}
