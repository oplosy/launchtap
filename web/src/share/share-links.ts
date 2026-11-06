/** Pure share-link helpers; safe for client components (no server configuration here). */

export function canonicalTokenUrl(origin: string, address: string): string {
  return `${origin.replace(/\/$/, "")}/token/${address.toLowerCase()}`;
}

/** `https://x.com/intent/post` link; every value is encoded by URLSearchParams. */
export function shareOnXUrl(symbol: string, canonicalUrl: string): string {
  const ticker = symbol.trim();
  const params = new URLSearchParams({
    text: ticker ? `$${ticker} on Launchpad` : "Token on Launchpad",
    url: canonicalUrl,
  });
  return `https://x.com/intent/post?${params.toString()}`;
}
