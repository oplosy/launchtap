// Display helpers for the engine-2 anti-snipe tax (Plan v2-C Task 3). They hold no tax
// arithmetic: the amount always comes from BondingCurveV2.quoteBuy (or the informational API
// quote), and only the countdown is derived here, from chain time (Plan v2-C D6).

/**
 * Seconds to add to the client clock to read chain time, measured from the latest block's
 * timestamp. Recomputed on every block read, so a skewed client clock never moves the
 * countdown.
 */
export function chainClockOffset(blockTimestamp: bigint, clientNowMs: number): number {
  return Number(blockTimestamp) - clientNowMs / 1000;
}

/** Whole seconds until the tax window ends at chain time `chainNowSeconds`; 0 when over. */
export function remainingTaxSeconds(
  endsAt: string | null | undefined,
  chainNowSeconds: number,
): number {
  if (!endsAt) return 0;
  const endsAtMs = Date.parse(endsAt);
  if (!Number.isFinite(endsAtMs)) return 0;
  return Math.max(0, Math.ceil(endsAtMs / 1000 - chainNowSeconds));
}

export function formatCountdown(seconds: number): string {
  const whole = Math.max(0, Math.floor(seconds));
  return `${Math.floor(whole / 60)}:${String(whole % 60).padStart(2, "0")}`;
}

function formatBps(bps: number): string {
  const whole = Math.floor(bps / 100);
  const fraction = bps % 100;
  return fraction === 0 ? `${whole}%` : `${whole}.${String(fraction).padStart(2, "0")}%`;
}

export type SnipeTaxLine = { amount: bigint; rate: string; timeLeft: string | null };

/** The quote summary's anti-snipe line, or null when the quote takes no tax. */
export function snipeTaxLine(args: {
  snipeTax: bigint;
  taxBps: number;
  remainingSeconds: number;
}): SnipeTaxLine | null {
  if (args.snipeTax <= 0n) return null;
  return {
    amount: args.snipeTax,
    rate: formatBps(args.taxBps),
    timeLeft: args.remainingSeconds > 0 ? formatCountdown(args.remainingSeconds) : null,
  };
}
