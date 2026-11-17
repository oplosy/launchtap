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

/** "Details edited 16 Nov 2026" from the API's `details_edited_at`, or null when never edited. */
export function detailsEditedLabel(iso: string | null | undefined): string | null {
  if (!iso) return null;
  const at = new Date(iso);
  if (Number.isNaN(at.getTime())) return null;
  const date = new Intl.DateTimeFormat("en-GB", {
    day: "numeric",
    month: "short",
    year: "numeric",
    timeZone: "UTC",
  }).format(at);
  return `Details edited ${date}`;
}
