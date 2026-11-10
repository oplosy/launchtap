const BASE_UNITS = /^\d+$/;

/** Direction of a spot-price change between two base-unit strings; null when unchanged or unreadable. */
export function priceDirection(previous: string, next: string): "up" | "down" | null {
  if (!BASE_UNITS.test(previous) || !BASE_UNITS.test(next)) return null;
  const before = BigInt(previous);
  const after = BigInt(next);
  if (after === before) return null;
  return after > before ? "up" : "down";
}

/** How long a price change stays highlighted. */
export const PRICE_FLASH_MS = 600;
