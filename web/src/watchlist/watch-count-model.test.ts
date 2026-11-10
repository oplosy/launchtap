import { describe, expect, it } from "vitest";
import { displayWatchCount, watchCountLabel } from "./watch-count-model";

describe("displayWatchCount", () => {
  it("shows the server count while the reader's state is unknown", () => {
    expect(displayWatchCount(4, null, false, false)).toBe(4);
    expect(displayWatchCount(4, null, true, true)).toBe(4);
    expect(displayWatchCount(4, true, false, false)).toBe(4);
  });

  it("adds or removes the reader's own toggle since the count was taken", () => {
    expect(displayWatchCount(4, false, true, true)).toBe(5);
    expect(displayWatchCount(4, true, true, false)).toBe(3);
    expect(displayWatchCount(4, true, true, true)).toBe(4);
    expect(displayWatchCount(0, true, true, false)).toBe(0);
  });

  it("labels the count", () => {
    expect(watchCountLabel(1234)).toBe("1,234 watching");
  });
});
