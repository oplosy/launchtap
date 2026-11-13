import { formatBaseUnits, parseDecimal } from "@/amounts";

// Mirrors internal/telegram MinTradeWei and MaxTradeWei (0.5 ETH and 10,000 ETH).
const MIN_TRADE_WEI = 500_000_000_000_000_000n;
const MAX_TRADE_WEI = 10_000n * 10n ** 18n;

/** The large-trade threshold typed in ETH, as decimal wei; null when invalid or out of range. */
export function thresholdToWei(input: string): string | null {
  let wei: bigint;
  try {
    wei = parseDecimal(input, 18);
  } catch {
    return null;
  }
  return wei >= MIN_TRADE_WEI && wei <= MAX_TRADE_WEI ? wei.toString() : null;
}

/** Decimal wei from the API as an ETH string for the input; "" when malformed. */
export function weiToThreshold(wei: string): string {
  if (!/^\d+$/.test(wei)) return "";
  return formatBaseUnits(BigInt(wei), 18);
}

/** The API's deep link is opened in a new tab; only https://t.me links are followed. */
export function isTelegramLink(url: string): boolean {
  try {
    const parsed = new URL(url);
    return parsed.protocol === "https:" && parsed.hostname === "t.me";
  } catch {
    return false;
  }
}

export function telegramProblemMessage(code: string | undefined): string {
  switch (code) {
    case "telegram_not_linked":
      return "Telegram is not connected any more. Connect it again.";
    case "invalid_threshold":
      return "Enter a trade size from 0.5 to 10,000 ETH.";
    default:
      return "Your Telegram settings could not be saved. Try again shortly.";
  }
}
