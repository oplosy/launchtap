import { describe, expect, it } from "vitest";
import { revealInitialState } from "./reveal";

describe("revealInitialState", () => {
  it("shows content at once when IntersectionObserver is unavailable", () => {
    expect(revealInitialState({ hasObserver: false, reducedMotion: false })).toBe("visible");
  });
  it("shows content at once under reduced motion", () => {
    expect(revealInitialState({ hasObserver: true, reducedMotion: true })).toBe("visible");
  });
  it("waits for the first intersection otherwise", () => {
    expect(revealInitialState({ hasObserver: true, reducedMotion: false })).toBe("pending");
  });
});
