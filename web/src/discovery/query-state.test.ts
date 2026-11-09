import { describe, expect, it } from "vitest";
import {
  decodeTokenListQuery,
  defaultTokenListQuery,
  encodeTokenListQuery,
  commitTokenListFilters,
  decodeExploreView,
  exploreViewSort,
  mergeOwnedParams,
  normalizeTokenListQuery,
  sameTokenListQuery,
  tokenListFilters,
} from "./query-state";

describe("token discovery URL state", () => {
  it("uses safe defaults and rejects unsupported API values", () => {
    expect(decodeTokenListQuery("?phase=unknown&sort=rank&q=%20%20")).toEqual(
      defaultTokenListQuery(),
    );
    expect(decodeTokenListQuery("", "graduated")).toEqual(defaultTokenListQuery("graduated"));
  });

  it("round trips supported filters and canonicalizes defaults", () => {
    const state = decodeTokenListQuery("?q=red%20%26%20blue&phase=graduated&sort=volume_24h");
    expect(encodeTokenListQuery(state)).toBe("q=red+%26+blue&phase=graduated&sort=volume_24h");
    expect(encodeTokenListQuery(defaultTokenListQuery())).toBe("");
    expect(sameTokenListQuery(state, decodeTokenListQuery(encodeTokenListQuery(state)))).toBe(true);
  });

  it("forces the graduated route to remain graduated and removes a conflicting URL phase", () => {
    const conflicting = decodeTokenListQuery("?phase=curve&sort=market_cap", "graduated");
    expect(conflicting.phase).toBe("curve");
    const normalized = normalizeTokenListQuery(conflicting, "graduated");
    expect(normalized.phase).toBe("graduated");
    expect(encodeTokenListQuery(normalized, "graduated")).toBe("sort=market_cap");
  });

  it("maps exactly to the generated GET /v1/tokens query", () => {
    expect(
      tokenListFilters({ q: "  coin ", phase: "curve", sort: "market_cap", age: "all" }),
    ).toEqual({ q: "  coin ", phase: "curve", sort: "market_cap", limit: 20 });
    expect(tokenListFilters({ q: "", phase: "curve", sort: "newest", age: "7d" })).toEqual({
      q: "",
      phase: "curve",
      sort: "newest",
      limit: 20,
      age: "7d",
    });
  });

  it("commits a pending search draft when another filter changes", () => {
    expect(
      commitTokenListFilters(
        { q: "old", phase: "curve", sort: "newest", age: "all" },
        "fresh search",
        { sort: "volume_24h" },
      ),
    ).toEqual({ q: "fresh search", phase: "curve", sort: "volume_24h", age: "all" });
  });
});

describe("explore view and age URL state", () => {
  it("round trips ?view=volume&age=7d", () => {
    const state = decodeTokenListQuery("?view=volume&age=7d");
    expect(state.age).toBe("7d");
    expect(decodeExploreView("?view=volume&age=7d")).toBe("volume");
    expect(encodeTokenListQuery(state)).toBe("age=7d");
    expect(decodeTokenListQuery(encodeTokenListQuery(state)).age).toBe("7d");
  });

  it("falls back to defaults for unknown values", () => {
    expect(decodeExploreView("?view=bogus")).toBe("trending");
    expect(decodeExploreView("")).toBe("trending");
    expect(decodeTokenListQuery("?age=forever").age).toBe("all");
  });

  it("omits age from the URL when it is all", () => {
    expect(encodeTokenListQuery({ ...defaultTokenListQuery(), age: "all" })).toBe("");
    expect(encodeTokenListQuery({ ...defaultTokenListQuery(), age: "24h" })).toBe("age=24h");
  });

  it("maps views to list sorts", () => {
    expect(exploreViewSort("new")).toBe("newest");
    expect(exploreViewSort("marketcap")).toBe("market_cap");
    expect(exploreViewSort("volume")).toBe("volume_24h");
    expect(exploreViewSort("trending")).toBeNull();
    expect(exploreViewSort("watchlist")).toBeNull();
  });

  it("merges owned params without touching the other section's params", () => {
    expect(mergeOwnedParams("q=red&view=volume", { view: "new", age: "7d" })).toBe(
      "q=red&view=new&age=7d",
    );
    expect(mergeOwnedParams("q=red&view=volume&age=7d", { q: null })).toBe("view=volume&age=7d");
    expect(mergeOwnedParams("view=volume", { view: null })).toBe("");
  });
});
