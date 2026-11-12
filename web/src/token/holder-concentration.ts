// Holder concentration from the holders list the API returns largest first with the curve,
// pool, locker and burn addresses excluded. Exact bigint math, rounded down to 0.01%, so the
// page never overstates or invents concentration.

const CANONICAL = /^(0|[1-9][0-9]*)$/;

export function positiveSupply(totalSupply: string): bigint | null {
  if (!CANONICAL.test(totalSupply)) return null;
  const supply = BigInt(totalSupply);
  return supply === 0n ? null : supply;
}

export function percentOfSupply(held: bigint, supply: bigint): string {
  const hundredths = (held * 10_000n) / supply;
  const fraction = (hundredths % 100n).toString().padStart(2, "0");
  return `${hundredths / 100n}.${fraction}%`;
}

/** Share of the whole supply held by the largest holders. */
export function topHolderShare(
  balances: readonly string[],
  totalSupply: string,
  count = 10,
): { holders: number; percent: string } | null {
  const supply = positiveSupply(totalSupply);
  if (!balances.length || supply === null) return null;
  const top = balances.slice(0, count);
  if (!top.every((balance) => CANONICAL.test(balance))) return null;
  const held = top.reduce((sum, balance) => sum + BigInt(balance), 0n);
  return { holders: top.length, percent: percentOfSupply(held, supply) };
}

/**
 * What the current holders who first acquired the token in its launch block still hold. Only a
 * buy bundled with the launch transaction's block lands there (the creator's own launch buy is one),
 * so this is the on-chain bundle/sniper signal. `first_acquired_block` resets when a holder sells
 * out, so it counts only wallets that have held since the launch block. With more pages to load,
 * the line says it covers the holders loaded so far.
 */
export function launchBlockHolders(
  holders: readonly { balance: string; first_acquired_block: number }[],
  launchBlock: number | undefined,
  totalSupply: string,
  hasMore: boolean,
): string | null {
  const supply = positiveSupply(totalSupply);
  if (!holders.length || supply === null) return null;
  if (launchBlock === undefined || !Number.isSafeInteger(launchBlock) || launchBlock < 0)
    return null;
  if (!holders.every((holder) => CANONICAL.test(holder.balance))) return null;
  const early = holders.filter((holder) => holder.first_acquired_block === launchBlock);
  if (!early.length)
    return hasMore
      ? "No holder loaded so far first got in at the launch block."
      : "No current holder first got in at the launch block.";
  const held = early.reduce((sum, holder) => sum + BigInt(holder.balance), 0n);
  const one = early.length === 1;
  return `${early.length} ${one ? "holder" : "holders"} first got in at the launch block (bundled with the launch, including any launch buy by the creator) and still ${one ? "holds" : "hold"} ${percentOfSupply(held, supply)} of the supply${hasMore ? ", among the holders loaded so far" : ""}.`;
}
