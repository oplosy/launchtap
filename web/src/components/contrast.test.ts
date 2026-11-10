import { describe, expect, it } from "vitest";
import { contrastRatio, THEME_TOKENS } from "./contrast";

describe("contrastRatio", () => {
  it("matches the WCAG reference values", () => {
    expect(contrastRatio("#000000", "#ffffff")).toBeCloseTo(21, 1);
    expect(contrastRatio("#ffffff", "#ffffff")).toBeCloseTo(1, 5);
    expect(contrastRatio("#777777", "#ffffff")).toBeCloseTo(4.48, 1);
  });
});

describe.each(["dark", "light"] as const)("%s theme tokens", (theme) => {
  const t = THEME_TOKENS[theme];
  it("keeps primary text at AAA on the canvas", () => {
    expect(contrastRatio(t.text, t.canvas)).toBeGreaterThanOrEqual(7);
  });
  it("keeps both grey text tones at AA on the canvas", () => {
    expect(contrastRatio(t.text2, t.canvas)).toBeGreaterThanOrEqual(4.5);
    expect(contrastRatio(t.text3, t.canvas)).toBeGreaterThanOrEqual(4.5);
  });
  it("keeps both grey text tones at AA on the fill (badges, inputs, secondary buttons)", () => {
    expect(contrastRatio(t.text2, t.fill)).toBeGreaterThanOrEqual(4.5);
    expect(contrastRatio(t.text3, t.fill)).toBeGreaterThanOrEqual(4.5);
  });
  it("keeps the data colours readable on the canvas", () => {
    expect(contrastRatio(t.up, t.canvas)).toBeGreaterThanOrEqual(3);
    expect(contrastRatio(t.down, t.canvas)).toBeGreaterThanOrEqual(3);
  });
});
