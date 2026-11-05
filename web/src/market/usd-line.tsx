"use client";

import { formatUsd, parseDecimal } from "@/amounts";
import { SafeExternalLink } from "@/components/primitives";
import { useEthUsd } from "./eth-usd";

/** Pure renderer: nothing unless there is a price and a valid non-negative amount. */
export function UsdLineView({
  wei,
  price,
  decimals = 18,
}: {
  wei: string | bigint;
  price: string | null;
  decimals?: number;
}) {
  if (!price) return null;
  let text: string;
  try {
    text = formatUsd(typeof wei === "bigint" ? wei : BigInt(wei), decimals, price);
  } catch {
    return null;
  }
  return <span className="usd-line">≈ {text}</span>;
}

/** Secondary muted USD figure to place under an ETH amount (wei, 18 decimals). */
export function UsdLine({ wei, decimals }: { wei: string | bigint; decimals?: number }) {
  const { price } = useEthUsd();
  return <UsdLineView wei={wei} price={price} decimals={decimals} />;
}

export function CoinGeckoAttribution() {
  const { price, attributionUrl } = useEthUsd();
  if (!price) return null;
  return attributionUrl ? (
    <SafeExternalLink href={attributionUrl}>Data provided by CoinGecko</SafeExternalLink>
  ) : (
    <span>Data provided by CoinGecko</span>
  );
}

/** USD estimate of an ETH amount typed by the user, plus the required data attribution. */
export function TradeUsdHint({ ethInput }: { ethInput?: string }) {
  const { price } = useEthUsd();
  if (!price) return null;
  let wei: bigint | null = null;
  try {
    wei = ethInput ? parseDecimal(ethInput, 18) : null;
  } catch {
    wei = null;
  }
  return (
    <small className="ui-field-hint usd-hint">
      {wei !== null ? <UsdLineView wei={wei} price={price} /> : null} <CoinGeckoAttribution />
    </small>
  );
}
