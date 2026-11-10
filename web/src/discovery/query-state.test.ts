import { describe, expect, it } from "vitest";
import {
  decodeTokenListQuery,
  defaultTokenListQuery,
  encodeTokenListQuery,
  commitTokenListFilters,
  decodeExploreView,
  EXPLORE_VIEWS,
  exploreListQuery,
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
  it("has exactly the four explore views", () => {
    expect(EXPLORE_VIEWS).toEqual(["trending", "new", "graduated", "watchlist"]);
  });

  it("round trips ?view=new&age=7d", () => {
    const state = decodeTokenListQuery("?view=new&age=7d");
    expect(state.age).toBe("7d");
    expect(decodeExploreView("?view=new&age=7d")).toBe("new");
    expect(encodeTokenListQuery(state)).toBe("age=7d");
    expect(decodeTokenListQuery(encodeTokenListQuery(state)).age).toBe("7d");
  });

  it("falls back to trending for unknown and legacy view values", () => {
    expect(decodeExploreView("?view=bogus")).toBe("trending");
    expect(decodeExploreView("?view=marketcap")).toBe("trending");
    expect(decodeExploreView("?view=volume")).toBe("trending");
    expect(decodeExploreView("")).toBe("trending");
    expect(decodeTokenListQuery("?age=forever").age).toBe("all");
  });

  it("omits age from the URL when it is all", () => {
    expect(encodeTokenListQuery({ ...defaultTokenListQuery(), age: "all" })).toBe("");
    expect(encodeTokenListQuery({ ...defaultTokenListQuery(), age: "24h" })).toBe("age=24h");
  });

  it("maps each view to the paged list it shows, or null for non-list views", () => {
    const list = (search: string) =>
      exploreListQuery(decodeExploreView(search), new URLSearchParams(search));
    expect(list("view=new")).toEqual({ phase: "curve", sort: "newest" });
    expect(list("view=new&sort=market_cap")).toEqual({ phase: "curve", sort: "newest" });
    expect(list("view=graduated")).toEqual({ phase: "graduated", sort: "newest" });
    expect(list("view=graduated&sort=volume_24h")).toEqual({
      phase: "graduated",
      sort: "volume_24h",
    });
    expect(list("view=graduated&sort=bogus")).toEqual({ phase: "graduated", sort: "newest" });
    // Trending is the highlights ranking unless a sort or a search turns it into a list.
    expect(list("")).toBeNull();
    expect(list("view=trending&sort=market_cap")).toEqual({ phase: "curve", sort: "market_cap" });
    expect(list("view=trending&sort=volume_24h")).toEqual({ phase: "curve", sort: "volume_24h" });
    expect(list("view=trending&sort=oldest")).toBeNull();
    expect(list("view=trending&q=dog")).toEqual({ phase: "curve", sort: "newest" });
    expect(list("view=watchlist&sort=market_cap")).toBeNull();
  });

  it("merges owned params without touching the other params", () => {
    expect(mergeOwnedParams("q=red&view=new", { view: "graduated", age: "7d" })).toBe(
      "q=red&view=graduated&age=7d",
    );
    expect(mergeOwnedParams("q=red&view=new&age=7d", { q: null })).toBe("view=new&age=7d");
    expect(mergeOwnedParams("view=new", { view: null })).toBe("");
  });
});
