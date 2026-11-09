import type { LiveEvent } from "@/api/types";
import { formatDisplayAmount } from "@/amounts";

/** Window over which live trade events for the wallet collapse into one overview refetch. */
export const WALLET_REFETCH_COALESCE_MS = 500;

export function walletPath(address: string): string {
  return `/wallet/${address.toLowerCase()}`;
}

/** True for a `trade` stream event whose trader is this wallet; a null trader never matches. */
export function isWalletTrade(event: LiveEvent, wallet: string): boolean {
  if (event.event !== "trade") return false;
  const trader = event.data.trader;
  return Boolean(trader) && trader?.toLowerCase() === wallet.toLowerCase();
}

function parseSigned(wei: string): bigint | null {
  if (!/^-?(?:0|[1-9]\d*)$/.test(wei)) return null;
  return BigInt(wei);
}

export function pnlTone(wei: string): "gain" | "loss" | "flat" {
  const value = parseSigned(wei);
  if (value === null || value === 0n) return "flat";
  return value > 0n ? "gain" : "loss";
}

/**
 * Signed ETH amount from a signed wei string; bigint math only, never a float. A non-zero amount
 * below display precision reads "+<0.0001 ETH" / "−<0.0001 ETH", never a signed zero.
 */
export function formatSignedEth(wei: string): string {
  const value = parseSigned(wei);
  if (value === null) return "Unavailable";
  if (value === 0n) return "0 ETH";
  const magnitude = formatDisplayAmount(value < 0n ? -value : value, 18, 4);
  return `${value < 0n ? "−" : "+"}${magnitude === "0" ? "<0.0001" : magnitude} ETH`;
}

/** USD-line input for a signed PnL: the magnitude in wei and whether it is a loss. */
export function pnlUsd(wei: string): { wei: string; negative: boolean } {
  const negative = pnlTone(wei) === "loss";
  return { wei: negative ? wei.slice(1) : wei, negative };
}

/** Runs `run` once, `ms` after the first trigger of a burst; later triggers join that run. */
export function createCoalescer(ms: number, run: () => void) {
  let timer: ReturnType<typeof setTimeout> | null = null;
  return {
    trigger() {
      if (timer !== null) return;
      timer = setTimeout(() => {
        timer = null;
        run();
      }, ms);
    },
    cancel() {
      if (timer !== null) clearTimeout(timer);
      timer = null;
    },
  };
}

/**
 * Delay of the one extra refetch after a live trigger. The live event can arrive before the
 * indexer's position worker has recomputed the wallet, so the first refetch may still read the
 * old figures; this second one picks up the recomputed ones.
 */
export const WALLET_REFETCH_FOLLOW_UP_MS = 3_000;

/**
 * Live-trigger refetch policy: a burst of triggers joins one run after `coalesceMs` (the existing
 * coalescer), and every such run schedules a single follow-up run `followUpMs` later; a newer run
 * replaces a pending follow-up rather than adding another. `cancel` drops both (unmount).
 */
export function createLiveRefetch(
  run: () => void,
  coalesceMs: number = WALLET_REFETCH_COALESCE_MS,
  followUpMs: number = WALLET_REFETCH_FOLLOW_UP_MS,
) {
  let followUp: ReturnType<typeof setTimeout> | null = null;
  const clearFollowUp = () => {
    if (followUp !== null) clearTimeout(followUp);
    followUp = null;
  };
  const coalescer = createCoalescer(coalesceMs, () => {
    run();
    clearFollowUp();
    followUp = setTimeout(() => {
      followUp = null;
      run();
    }, followUpMs);
  });
  return {
    trigger: () => coalescer.trigger(),
    cancel() {
      coalescer.cancel();
      clearFollowUp();
    },
  };
}

export type CursorPage<T> = { items: T[] | null; next_cursor?: string };

/**
 * Reloads a cursor-paged list from page one up to at least `loadedCount` rows (the rows already on
 * screen), following cursors one page at a time so each request stays within the API page limit.
 * Rows repeated across overlapping pages are dropped. Rejects when `signal` aborts.
 */
export async function reloadLoadedPages<T>(
  fetchPage: (cursor: string | undefined, signal: AbortSignal) => Promise<CursorPage<T>>,
  identity: (item: T) => string,
  loadedCount: number,
  signal: AbortSignal,
): Promise<{ items: T[]; next: string | null }> {
  const items: T[] = [];
  const seen = new Set<string>();
  let cursor: string | undefined;
  do {
    const page = await fetchPage(cursor, signal);
    signal.throwIfAborted();
    const before = items.length;
    for (const item of page.items ?? []) {
      const key = identity(item);
      if (seen.has(key)) continue;
      seen.add(key);
      items.push(item);
    }
    cursor = page.next_cursor || undefined;
    // A page that adds nothing new cannot make progress; stop rather than loop on its cursor.
    if (items.length === before) break;
  } while (cursor && items.length < loadedCount);
  return { items, next: cursor ?? null };
}
