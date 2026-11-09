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

/** Signed ETH amount from a signed wei string; bigint math only, never a float. */
export function formatSignedEth(wei: string): string {
  const value = parseSigned(wei);
  if (value === null) return "Unavailable";
  if (value === 0n) return "0 ETH";
  const magnitude = formatDisplayAmount(value < 0n ? -value : value, 18, 4);
  return `${value < 0n ? "−" : "+"}${magnitude} ETH`;
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
