import { describe, expect, it, vi } from "vitest";
import { LEADERBOARD_TIMEOUT_MS, loadLeaderboard } from "./leaderboard-data";

const BASE = "http://api:8080";
const snapshot = { chain_id: 1, as_of_block: 1, as_of_block_hash: "0xabc", finality: "safe" };

describe("loadLeaderboard", () => {
  it("returns the body and asks for 100 rows with a 60 s data-cache lifetime", async () => {
    const fetcher = vi.fn(async () => Response.json({ snapshot, items: [] }));
    await expect(loadLeaderboard(fetcher as unknown as typeof fetch, BASE)).resolves.toEqual({
      snapshot,
      items: [],
    });
    const [url, init] = fetcher.mock.calls[0] as unknown as [
      string,
      RequestInit & { next?: object },
    ];
    expect(url).toBe(`${BASE}/v1/leaderboard?limit=100`);
    expect(init.next).toEqual({ revalidate: 60 });
    expect(init.signal).toBeInstanceOf(AbortSignal);
    expect(LEADERBOARD_TIMEOUT_MS).toBe(3_000);
  });

  // Throwing (instead of returning an "unavailable" value) lets Next keep the last good render or
  // cached data; only a route that never rendered falls through to its error boundary.
  it("throws on a non-OK response", async () => {
    const fetcher = vi.fn(async () => new Response("x", { status: 503 }));
    await expect(loadLeaderboard(fetcher as unknown as typeof fetch, BASE)).rejects.toThrow(
      "leaderboard: API responded 503",
    );
  });

  it("throws on a network failure", async () => {
    const fetcher = vi.fn(async () => {
      throw new TypeError("fetch failed");
    });
    await expect(loadLeaderboard(fetcher as unknown as typeof fetch, BASE)).rejects.toThrow(
      "fetch failed",
    );
  });

  it("throws on a malformed body", async () => {
    const fetcher = vi.fn(async () => Response.json({ items: "nope" }));
    await expect(loadLeaderboard(fetcher as unknown as typeof fetch, BASE)).rejects.toThrow(
      "leaderboard: malformed response",
    );
  });
});
