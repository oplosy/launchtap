import { formatCanonicalBaseUnits, formatSignificantPrice } from "@/amounts";

export function amount(raw: string) {
  const value = formatCanonicalBaseUnits(raw, 5);
  return value === null ? "Unavailable" : `${value} ETH`;
}

export function finalityTone(value: string): "success" | "warning" | "neutral" {
  return value === "safe" || value === "finalized"
    ? "success"
    : value === "provisional" || value === "stale"
      ? "warning"
      : "neutral";
}

export function finalityLabel(value: string) {
  return value === "safe" || value === "finalized" ? "Verified" : "Updating";
}

export function safeTokenAmount(raw: string) {
  return formatCanonicalBaseUnits(raw, 3) ?? "Unavailable";
}

export function safeMarketValue(raw: string) {
  return formatSignificantPrice(raw) ?? "Unavailable";
}
