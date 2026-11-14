/** Resolve API-owned relative image paths without allowing a metadata URL to change origin. */
export function resolveApiAssetUrl(
  baseUrl: string | null | undefined,
  value: string | null | undefined,
) {
  if (!value) return null;
  if (parseUrl(value)?.protocol === "https:") return new URL(value).toString();
  if (!baseUrl || !value.startsWith("/")) return null;
  const base = parseUrl(baseUrl);
  if (!base) return null;
  try {
    const resolved = new URL(value, base);
    return resolved.origin === base.origin &&
      (base.protocol === "https:" || isLocalHost(base.hostname))
      ? resolved.toString()
      : null;
  } catch {
    return null;
  }
}

function parseUrl(value: string): URL | null {
  try {
    return new URL(value);
  } catch {
    return null;
  }
}

function isLocalHost(hostname: string) {
  return hostname === "localhost" || hostname === "127.0.0.1" || hostname === "[::1]";
}
