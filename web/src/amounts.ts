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

/**
 * Execution price in wad (ETH per whole token, 18 decimals) from a trade's gross ETH and token
 * amounts, as a canonical decimal string. Mirrors the `market_trades` view's
 * `execution_price_wad` (backend migration 00004_chain_projections.sql):
 * `trunc(eth_gross * 1e18 / NULLIF(token_amount, 0))`. Empty when there are no tokens (the view
 * yields NULL) or an amount is not a canonical integer.
 */
export function executionPriceWad(ethWei: string, tokenUnits: string): string {
  let eth: bigint;
  let tokens: bigint;
  try {
    eth = parseCanonicalBaseUnits(ethWei);
    tokens = parseCanonicalBaseUnits(tokenUnits);
  } catch {
    return "";
  }
  if (tokens === 0n) return "";
  return ((eth * 10n ** 18n) / tokens).toString();
}

/** Token prices sit far below 1 ETH, so keep four significant digits instead of a fixed scale. */
export function formatSignificantPrice(value: string): string | null {
  const full = formatCanonicalBaseUnits(value, 18);
  if (full === null) return null;
  const leadingZeros = (full.split(".")[1] ?? "").search(/[1-9]/);
  const digits = leadingZeros < 0 ? 6 : Math.min(18, Math.max(6, leadingZeros + 4));
  return formatCanonicalBaseUnits(value, digits);
}

/** Fraction zeros from which a price is shown as `0.0₈1699`; fewer still read fine in full. */
export const COMPACT_ZERO_MIN = 4;
const SUBSCRIPT_DIGITS = "₀₁₂₃₄₅₆₇₈₉";

/**
 * Display only: writes a run of leading fraction zeros as a subscript count, so
 * "0.000000001699" reads "0.0₈1699". Any other string is returned unchanged.
 */
export function compactZeros(decimal: string): string {
  const match = /^0\.(0+)([1-9]\d*)$/.exec(decimal);
  if (!match || match[1]!.length < COMPACT_ZERO_MIN) return decimal;
  const count = [...String(match[1]!.length)].map((digit) => SUBSCRIPT_DIGITS[Number(digit)]);
  return `0.0${count.join("")}${match[2]}`;
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

/**
 * Exact USD value of a base-unit amount at a decimal USD price (e.g. ETH/USD).
 * >= $1 shows 2 decimals with thousands separators; < $1 shows 4 significant
 * digits. Half-up rounding on bigint math, never exponent notation.
 */
export function formatUsd(baseUnits: bigint, decimals: number, priceUsd: string): string {
  if (baseUnits < 0n || !Number.isInteger(decimals) || decimals < 0 || decimals > 255)
    throw new Error("Invalid USD amount");
  const price = priceUsd.trim();
  const priceDecimals = (price.split(".")[1] ?? "").length;
  const numerator = baseUnits * parseDecimal(price, priceDecimals);
  const denominator = 10n ** BigInt(decimals + priceDecimals);
  if (numerator === 0n) return "$0.00";
  const roundDiv = (n: bigint, d: bigint) => (2n * n + d) / (2n * d);
  const twoDecimals = (cents: bigint) => {
    const whole = (cents / 100n).toString().replace(/\B(?=(\d{3})+(?!\d))/g, ",");
    return `$${whole}.${(cents % 100n).toString().padStart(2, "0")}`;
  };
  if (numerator >= denominator) return twoDecimals(roundDiv(numerator * 100n, denominator));
  let zeros = 0;
  while (numerator * 10n ** BigInt(zeros + 1) < denominator) zeros++;
  const fractionDigits = zeros + 4;
  const scaled = roundDiv(numerator * 10n ** BigInt(fractionDigits), denominator);
  if (scaled >= 10n ** BigInt(fractionDigits)) return "$1.00";
  return `$0.${scaled.toString().padStart(fractionDigits, "0")}`;
}

export function formatDisplayAmount(value: bigint, decimals = 18, maxFractionDigits = 4): string {
  const raw = formatBaseUnits(value, decimals, maxFractionDigits);
  const [whole = "", fraction] = raw.split(".");
  const sign = whole.startsWith("-") ? "-" : "";
  const digits = sign ? whole.slice(1) : whole;
  const grouped = digits.replace(/\B(?=(\d{3})+(?!\d))/g, ",");
  return fraction ? `${sign}${grouped}.${fraction}` : `${sign}${grouped}`;
}
