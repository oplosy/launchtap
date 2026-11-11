import { describe, expect, it } from "vitest";
import { layoutCells, nextPost, pickCell, placeInCell } from "./forum-field-model";

/** Deterministic stand-in for Math.random: walks through the given values in a loop. */
function sequence(...values: number[]) {
  let index = 0;
  return (): number => values[index++ % values.length] ?? 0;
}

describe("layoutCells", () => {
  it("splits the field into as many card-sized cells as fit, sharing the spare room", () => {
    const cells = layoutCells({ width: 1296, height: 624 }, { w: 384, h: 176 }, 16);
    expect(cells).toHaveLength(9);
    expect(cells[0]).toEqual({ x: 0, y: 0, w: 432, h: 208 });
    expect(cells[8]).toEqual({ x: 864, y: 416, w: 432, h: 208 });
  });

  it("has one column on a narrow field", () => {
    const cells = layoutCells({ width: 358, height: 760 }, { w: 358, h: 176 }, 16);
    expect(cells.map((cell) => cell.x)).toEqual([0, 0, 0, 0]);
  });

  it("has no cells when a card does not fit", () => {
    expect(layoutCells({ width: 100, height: 600 }, { w: 384, h: 176 }, 16)).toEqual([]);
  });
});

describe("placeInCell", () => {
  const cell = { x: 432, y: 208, w: 432, h: 208 };
  const size = { w: 384, h: 176 };
  it("shifts the card at random inside its cell, never past its edges", () => {
    expect(placeInCell(cell, size, sequence(0, 0))).toEqual({ x: 432, y: 208, w: 384, h: 176 });
    expect(placeInCell(cell, size, sequence(1, 1))).toEqual({ x: 480, y: 240, w: 384, h: 176 });
  });
});

describe("pickCell", () => {
  it("picks a free cell other than the one just left", () => {
    // Cells 0 and 2 are taken and 1 was just left, so only 3 remains.
    expect(pickCell(4, new Set([0, 2]), 1, sequence(0))).toBe(3);
  });

  it("falls back to the cell just left when it is the only free one", () => {
    expect(pickCell(3, new Set([0, 2]), 1, sequence(0.5))).toBe(1);
  });

  it("returns null when every cell is taken", () => {
    expect(pickCell(2, new Set([0, 1]), null, Math.random)).toBeNull();
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
