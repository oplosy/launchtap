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

/**
 * The public site origin (NEXT_PUBLIC_WEB_ORIGIN) or null. Server metadata and the client share
 * buttons both use this one function so they always agree; no request or window fallback.
 */
export function publicWebOrigin(
  value: string | undefined = process.env.NEXT_PUBLIC_WEB_ORIGIN,
): string | null {
  const trimmed = value?.trim();
  if (!trimmed) return null;
  try {
    const url = new URL(trimmed);
    if (url.protocol !== "http:" && url.protocol !== "https:") return null;
    if (url.username || url.password || url.search || url.hash) return null;
    return url.origin;
  } catch {
    return null;
  }
}
