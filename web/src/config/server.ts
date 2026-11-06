/**
 * Server-only settings. Never import this module from a client component: it reads private
 * environment variables that must not reach the browser bundle.
 */

/** Strip a single trailing slash and reject anything that is not a plain http(s) origin/path. */
function httpBase(value: string, key: string): string {
  let url: URL;
  try {
    url = new URL(value);
  } catch {
    throw new Error(`${key} is not a valid URL`);
  }
  if (url.protocol !== "http:" && url.protocol !== "https:")
    throw new Error(`${key} must use http or https`);
  if (url.username || url.password || url.search || url.hash)
    throw new Error(`${key} contains credentials or query material`);
  return url.toString().replace(/\/$/, "");
}

/**
 * Base URL the web container uses to reach our own API: API_INTERNAL_BASE_URL, falling back to
 * NEXT_PUBLIC_API_BASE_URL. Throws when neither is set or the value is not http(s).
 */
export function apiInternalBaseUrl(
  env: Record<string, string | undefined> = {
    API_INTERNAL_BASE_URL: process.env.API_INTERNAL_BASE_URL,
    NEXT_PUBLIC_API_BASE_URL: process.env.NEXT_PUBLIC_API_BASE_URL,
  },
): string {
  const internal = env.API_INTERNAL_BASE_URL?.trim();
  if (internal) return httpBase(internal, "API_INTERNAL_BASE_URL");
  const publicBase = env.NEXT_PUBLIC_API_BASE_URL?.trim();
  if (publicBase) return httpBase(publicBase, "NEXT_PUBLIC_API_BASE_URL");
  throw new Error("API_INTERNAL_BASE_URL or NEXT_PUBLIC_API_BASE_URL is required");
}
