import type {
  WalletCreatedResponse,
  WalletPositionsResponse,
  WalletTradesResponse,
} from "@/api/client";
import { formatCanonicalBaseUnits } from "@/amounts";
import { UsdLine } from "@/market/usd-line";
import { formatSignedEth, pnlTone, pnlUsd } from "./wallet-model";

export type Position = NonNullable<WalletPositionsResponse["items"]>[number];
export type WalletTrade = NonNullable<WalletTradesResponse["items"]>[number];
export type Created = NonNullable<WalletCreatedResponse["items"]>[number];
export type Page<T> = { items: T[] | null; next_cursor?: string };

export function eth(wei: string) {
  const formatted = formatCanonicalBaseUnits(wei, 4);
  return formatted === null ? "Unavailable" : `${formatted} ETH`;
}
export function tokenAmount(raw: string) {
  return formatCanonicalBaseUnits(raw, 3) ?? "Unavailable";
}
export function safeDate(value: string) {
  const date = new Date(value);
  return Number.isNaN(date.valueOf())
    ? "Unavailable"
    : date.toLocaleString("en-US", { dateStyle: "medium", timeStyle: "short" });
}
export function isTransfer(transferredIn: string) {
  return /^[1-9]\d*$/.test(transferredIn);
}
export function Pnl({ wei }: { wei: string }) {
  const usd = pnlUsd(wei);
  return (
    <span className={`pnl pnl-${pnlTone(wei)}`}>
      <span className="pnl-value">{formatSignedEth(wei)}</span>
      <UsdLine wei={usd.wei} negative={usd.negative} />
    </span>
  );
}
