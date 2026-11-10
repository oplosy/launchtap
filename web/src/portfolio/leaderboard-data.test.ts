import { describe, expect, it, vi } from "vitest";
import {
  LEADERBOARD_TIMEOUT_MS,
  loadLeaderboard,
  parseLeaderboardWindow,
} from "./leaderboard-data";

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

describe("leaderboard windows", () => {
  it("parses only the supported window values", () => {
    expect(parseLeaderboardWindow("7d")).toBe("7d");
    expect(parseLeaderboardWindow("30d")).toBe("30d");
    expect(parseLeaderboardWindow("all")).toBe("all");
    for (const raw of [undefined, "", "24h", "7D", ["7d", "30d"]]) {
      expect(parseLeaderboardWindow(raw)).toBe("all");
    }
  });

  it("asks the API for the window, and omits it for all time", async () => {
    const fetcher = vi.fn(async () => Response.json({ snapshot, items: [] }));
    await loadLeaderboard(fetcher as unknown as typeof fetch, BASE, "30d");
    await loadLeaderboard(fetcher as unknown as typeof fetch, BASE, "all");
    const urls = fetcher.mock.calls.map((call) => (call as unknown as [string])[0]);
    expect(urls).toEqual([
      `${BASE}/v1/leaderboard?limit=100&window=30d`,
      `${BASE}/v1/leaderboard?limit=100`,
    ]);
  });
});
