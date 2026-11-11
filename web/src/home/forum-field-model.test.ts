import { describe, expect, it } from "vitest";
import { nextPost, overlaps, pickSpot, type Spot } from "./forum-field-model";

/** Deterministic stand-in for Math.random: walks through the given values in a loop. */
function sequence(...values: number[]) {
  let index = 0;
  return (): number => values[index++ % values.length] ?? 0;
}

describe("overlaps", () => {
  const a: Spot = { x: 0, y: 0, w: 100, h: 50 };
  it("counts the gap between two boxes as taken", () => {
    expect(overlaps(a, { x: 110, y: 0, w: 100, h: 50 }, 16)).toBe(true);
    expect(overlaps(a, { x: 120, y: 0, w: 100, h: 50 }, 16)).toBe(false);
    expect(overlaps(a, { x: 0, y: 70, w: 100, h: 50 }, 16)).toBe(false);
  });
});

describe("pickSpot", () => {
  const area = { width: 400, height: 200 };
  const size = { w: 100, h: 50 };

  it("keeps the box inside the area", () => {
    const spot = pickSpot([], area, size, sequence(0.999, 0.999));
    expect(spot).toEqual({ x: 300, y: 150, w: 100, h: 50 });
  });

  it("skips spots that overlap a box already shown", () => {
    const taken: Spot[] = [{ x: 0, y: 0, w: 100, h: 50 }];
    // First try lands on the taken box, the second one is clear.
    const spot = pickSpot(taken, area, size, sequence(0, 0, 0.75, 0.75));
    expect(spot).toEqual({ x: 225, y: 113, w: 100, h: 50 });
  });

  it("gives up when no free spot is found", () => {
    const taken: Spot[] = [{ x: 0, y: 0, w: 400, h: 200 }];
    expect(pickSpot(taken, area, size, Math.random)).toBeNull();
  });

  it("gives up when the box does not fit the area at all", () => {
    expect(pickSpot([], { width: 50, height: 200 }, size, Math.random)).toBeNull();
  });
});

describe("nextPost", () => {
  const posts = [{ id: "c" }, { id: "b" }, { id: "a" }];

  it("shows posts never shown before first, newest first", () => {
    expect(nextPost(posts, new Set(), new Map([["c", 5]]))?.id).toBe("b");
  });

  it("never repeats a post that is on screen", () => {
    expect(nextPost(posts, new Set(["c", "b"]), new Map())?.id).toBe("a");
  });

  it("then cycles through the posts shown longest ago", () => {
    const shown = new Map([
      ["c", 30],
      ["b", 10],
      ["a", 20],
    ]);
    expect(nextPost(posts, new Set(), shown)?.id).toBe("b");
  });

  it("returns null when every post is already on screen", () => {
    expect(nextPost(posts, new Set(["a", "b", "c"]), new Map())).toBeNull();
  });
});
