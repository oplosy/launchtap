import { describe, expect, it } from "vitest";
import { progressPercent } from "./highlights";

describe("progressPercent", () => {
  it("formats basis points as a one-decimal percentage", () => {
    expect(progressPercent(8000)).toBe("80.0%");
    expect(progressPercent(1234)).toBe("12.3%");
    expect(progressPercent(0)).toBe("0.0%");
  });

  it("clamps out-of-range and invalid values", () => {
    expect(progressPercent(12_000)).toBe("100.0%");
    expect(progressPercent(-5)).toBe("0.0%");
    expect(progressPercent(Number.NaN)).toBe("0.0%");
  });
});
