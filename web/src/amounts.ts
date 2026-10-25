const DECIMAL = /^\d+(?:\.\d+)?$/;
const INTEGER = /^\d+$/;
const CANONICAL_INTEGER = /^(?:0|[1-9]\d*)$/;

export function parseBaseUnits(value: string, decimals = 18): bigint {
  if (!Number.isInteger(decimals) || decimals < 0 || decimals > 255 || !INTEGER.test(value))
    throw new Error("Invalid integer amount");
  return BigInt(value);
}

export function parseCanonicalBaseUnits(value: string, decimals = 18): bigint {
  if (
    !Number.isInteger(decimals) ||
    decimals < 0 ||
    decimals > 255 ||
    !CANONICAL_INTEGER.test(value)
  )
    throw new Error("Invalid integer amount");
  return BigInt(value);
}

/** Convert a strict WAD integer into a bounded chart number only after bigint scaling. */
export function wadToBoundedNumber(value: string, maxWhole = 1_000_000_000_000): number | null {
  if (!Number.isSafeInteger(maxWhole) || maxWhole < 0) return null;
  let wad: bigint;
  try {
    wad = parseCanonicalBaseUnits(value);
  } catch {
    return null;
  }
  const scale = 10n ** 18n;
  const whole = wad / scale;
  if (whole > BigInt(maxWhole)) return null;
  const fraction = wad % scale;
  const result = Number(whole) + Number(fraction) / 1e18;
  return Number.isFinite(result) ? result : null;
}

export function formatCanonicalBaseUnits(value: string, maxFractionDigits = 6): string | null {
  try {
    return formatDisplayAmount(parseCanonicalBaseUnits(value), 18, maxFractionDigits);
  } catch {
    return null;
  }
}

/** Token prices sit far below 1 ETH, so keep four significant digits instead of a fixed scale. */
export function formatSignificantPrice(value: string): string | null {
  const full = formatCanonicalBaseUnits(value, 18);
  if (full === null) return null;
  const leadingZeros = (full.split(".")[1] ?? "").search(/[1-9]/);
  const digits = leadingZeros < 0 ? 6 : Math.min(18, Math.max(6, leadingZeros + 4));
  return formatCanonicalBaseUnits(value, digits);
}

export function parseDecimal(value: string, decimals = 18): bigint {
  if (!Number.isInteger(decimals) || decimals < 0 || decimals > 255 || !DECIMAL.test(value.trim()))
    throw new Error("Invalid decimal amount");
  const normalized = value.trim();
  const [whole = "", fraction = ""] = normalized.split(".");
  if (fraction.length > decimals) throw new Error("Too many decimal places");
  return (
    BigInt(whole) * 10n ** BigInt(decimals) +
    BigInt((fraction + "0".repeat(decimals)).slice(0, decimals) || "0")
  );
}

export function formatBaseUnits(
  value: bigint,
  decimals = 18,
  maxFractionDigits = decimals,
): string {
  if (
    !Number.isInteger(decimals) ||
    decimals < 0 ||
    decimals > 255 ||
    !Number.isInteger(maxFractionDigits) ||
    maxFractionDigits < 0 ||
    maxFractionDigits > decimals
  )
    throw new Error("Invalid decimals");
  if (value < 0n) return `-${formatBaseUnits(-value, decimals, maxFractionDigits)}`;
  const scale = 10n ** BigInt(decimals);
  const whole = value / scale;
  if (decimals === 0 || maxFractionDigits === 0) return whole.toString();
  const fraction = (value % scale)
    .toString()
    .padStart(decimals, "0")
    .slice(0, maxFractionDigits)
    .replace(/0+$/, "");
  return fraction ? `${whole}.${fraction}` : whole.toString();
}

export function formatDisplayAmount(value: bigint, decimals = 18, maxFractionDigits = 4): string {
  const raw = formatBaseUnits(value, decimals, maxFractionDigits);
  const [whole = "", fraction] = raw.split(".");
  const sign = whole.startsWith("-") ? "-" : "";
  const digits = sign ? whole.slice(1) : whole;
  const grouped = digits.replace(/\B(?=(\d{3})+(?!\d))/g, ",");
  return fraction ? `${sign}${grouped}.${fraction}` : `${sign}${grouped}`;
}
