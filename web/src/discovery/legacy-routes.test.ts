import { describe, expect, it } from "vitest";
import { legacyExploreTarget } from "./legacy-routes";

const target = (search: string) => legacyExploreTarget(new URLSearchParams(search));

describe("legacyExploreTarget", () => {
  it("returns null without a view, so the home page renders", () => {
    expect(target("")).toBeNull();
    expect(target("q=dog")).toBeNull();
  });
  it("moves a current view to /explore", () => {
    expect(target("view=new")).toBe("/explore?view=new");
    expect(target("view=watchlist")).toBe("/explore?view=watchlist");
    expect(target("view=graduated")).toBe("/explore?view=graduated");
  });
  it("turns the old ranked views into trending with the matching sort", () => {
    expect(target("view=marketcap&q=dog")).toBe("/explore?view=trending&sort=market_cap&q=dog");
    expect(target("view=volume")).toBe("/explore?view=trending&sort=volume_24h");
  });
  it("carries q and age and drops everything else", () => {
    expect(target("view=new&age=7d&q=cat&utm=x")).toBe("/explore?view=new&q=cat&age=7d");
  });
  it("sends unknown views to trending", () => {
    expect(target("view=bogus")).toBe("/explore?view=trending");
  });
});
