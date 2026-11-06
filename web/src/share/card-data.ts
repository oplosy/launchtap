/**
 * Share-card data loader. Server-side only (reads the private API base); no React, no Next.
 * Token names, symbols and images are user input: text is returned as plain strings and the
 * image is fetched only from our own API image route, never from a URL taken from metadata.
 */
import { formatCanonicalBaseUnits, formatUsd, parseCanonicalBaseUnits } from "@/amounts";
import { apiInternalBaseUrl } from "@/config/server";
import { parseTokenAddress } from "@/token/address";

export type CardData =
  | {
      kind: "token";
      name: string;
      symbol: string;
      /** The API image route, set only when a bounded, format-checked fetch of it succeeded. */
      imageUrl: string | null;
      /** The verified image bytes as a data URI (what the renderer embeds), or null. */
      imageDataUri: string | null;
      marketCapEth: string;
      marketCapUsd: string | null;
      phase: "bonding" | "graduated";
      progressBps: number;
    }
  | { kind: "generic" };

export const FETCH_TIMEOUT_MS = 3000;
export const MAX_NAME_LENGTH = 32;
const MAX_SYMBOL_LENGTH = 16;
/** Mirrors backend maxImageBytes (metadata_routes.go): 5 MiB. */
export const MAX_IMAGE_BYTES = 5 << 20;
/** Upload path accepts PNG, JPEG and WebP; the renderer can only embed PNG and JPEG. */

type Fetcher = typeof fetch;

function clean(value: unknown, max: number): string {
  if (typeof value !== "string") return "";
  // Plain text only: drop control characters and collapse whitespace.
  const text = value
    .replace(/[\u0000-\u001f\u007f]+/g, " ")
    .replace(/\s+/g, " ")
    .trim();
  const chars = Array.from(text);
  return chars.length > max ? `${chars.slice(0, max - 1).join("")}…` : text;
}

function detectImageType(bytes: Uint8Array): "image/png" | "image/jpeg" | null {
  if (
    bytes.length > 8 &&
    bytes[0] === 0x89 &&
    bytes[1] === 0x50 &&
    bytes[2] === 0x4e &&
    bytes[3] === 0x47
  )
    return "image/png";
  if (bytes.length > 3 && bytes[0] === 0xff && bytes[1] === 0xd8 && bytes[2] === 0xff)
    return "image/jpeg";
  return null;
}

async function readBounded(response: Response, limit: number): Promise<Uint8Array | null> {
  const declared = Number(response.headers.get("content-length"));
  if (Number.isFinite(declared) && declared > limit) return null;
  if (!response.body) return null;
  const reader = response.body.getReader();
  const chunks: Uint8Array[] = [];
  let total = 0;
  for (;;) {
    const { done, value } = await reader.read();
    if (done) break;
    total += value.length;
    if (total > limit) {
      await reader.cancel();
      return null;
    }
    chunks.push(value);
  }
  const bytes = new Uint8Array(total);
  let offset = 0;
  for (const chunk of chunks) {
    bytes.set(chunk, offset);
    offset += chunk.length;
  }
  return bytes;
}

/** Fetch the token image from our API route only; null on any failure or unsupported format. */
async function loadImage(
  imageUrl: string,
  fetcher: Fetcher,
): Promise<{ imageUrl: string; imageDataUri: string } | null> {
  try {
    const response = await fetcher(imageUrl, {
      signal: AbortSignal.timeout(FETCH_TIMEOUT_MS),
      redirect: "error",
      headers: { Accept: "image/png, image/jpeg" },
    });
    if (!response.ok) return null;
    const bytes = await readBounded(response, MAX_IMAGE_BYTES);
    if (!bytes) return null;
    const type = detectImageType(bytes);
    if (!type) return null;
    return {
      imageUrl,
      imageDataUri: `data:${type};base64,${Buffer.from(bytes).toString("base64")}`,
    };
  } catch (cause) {
    console.error("share card: token image unavailable", cause);
    return null;
  }
}

async function getJson(url: string, fetcher: Fetcher): Promise<unknown> {
  const response = await fetcher(url, {
    signal: AbortSignal.timeout(FETCH_TIMEOUT_MS),
    redirect: "error",
    headers: { Accept: "application/json" },
  });
  if (!response.ok) throw new Error(`API responded ${response.status}`);
  return response.json();
}

async function loadEthUsd(base: string, fetcher: Fetcher): Promise<string | null> {
  try {
    const body = (await getJson(`${base}/v1/reference/eth-usd`, fetcher)) as { price?: unknown };
    return typeof body.price === "string" && body.price ? body.price : null;
  } catch (cause) {
    console.error("share card: ETH/USD reference unavailable", cause);
    return null;
  }
}

export async function loadCardData(
  address: string,
  fetcher: Fetcher = fetch,
  baseUrl?: string,
): Promise<CardData> {
  const parsed = parseTokenAddress(address);
  if (!parsed) return { kind: "generic" };
  try {
    const base = baseUrl ?? apiInternalBaseUrl();
    const raw = (await getJson(`${base}/v1/tokens/${parsed}`, fetcher)) as Record<string, unknown>;
    const name = clean(raw.name, MAX_NAME_LENGTH);
    const symbol = clean(raw.symbol, MAX_SYMBOL_LENGTH);
    const marketCapRaw = raw.market_cap_eth;
    if (typeof marketCapRaw !== "string") throw new Error("token response is malformed");
    const marketCapEth = formatCanonicalBaseUnits(marketCapRaw, 2);
    if (marketCapEth === null) throw new Error("market cap is not a canonical amount");
    const bps = Number(raw.graduation_progress_bps);
    const phase = raw.phase === "graduated" ? "graduated" : "bonding";
    const [price, image] = await Promise.all([
      loadEthUsd(base, fetcher),
      loadImage(`${base}/v1/tokens/${parsed}/image`, fetcher),
    ]);
    let marketCapUsd: string | null = null;
    if (price) {
      try {
        marketCapUsd = formatUsd(parseCanonicalBaseUnits(marketCapRaw), 18, price);
      } catch {
        marketCapUsd = null;
      }
    }
    return {
      kind: "token",
      name: name || symbol || "Unnamed token",
      symbol,
      imageUrl: image?.imageUrl ?? null,
      imageDataUri: image?.imageDataUri ?? null,
      marketCapEth,
      marketCapUsd,
      phase,
      progressBps: Number.isFinite(bps) ? Math.min(10_000, Math.max(0, Math.round(bps))) : 0,
    };
  } catch (cause) {
    console.error("share card: falling back to the generic card", cause);
    return { kind: "generic" };
  }
}
