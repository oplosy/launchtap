import { parseTokenAddress } from "./address";

type MarketLinkSource = {
  marketLinks?: { dexscreenerChain?: string; geckoterminalNetwork?: string } | null;
} | null;

const IDENTIFIER = /^[a-z0-9-]{1,32}$/;

/** Outbound market-data links; only for graduated tokens with a valid pair and safe identifiers. */
export function marketLinks(
  deployment: MarketLinkSource | undefined,
  pair: string | null,
  phase: string,
): { dexscreener?: string; geckoterminal?: string } {
  const links: { dexscreener?: string; geckoterminal?: string } = {};
  const address = parseTokenAddress(pair);
  const config = deployment?.marketLinks;
  if (phase !== "graduated" || !address || !config) return links;
  const chain = config.dexscreenerChain;
  if (chain && IDENTIFIER.test(chain)) {
    links.dexscreener = `https://dexscreener.com/${chain}/${address}`;
  }
  const network = config.geckoterminalNetwork;
  if (network && IDENTIFIER.test(network)) {
    links.geckoterminal = `https://www.geckoterminal.com/${network}/pools/${address}`;
  }
  return links;
}
