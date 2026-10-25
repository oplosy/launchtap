import { describe, expect, it } from "vitest";
import { CURVE_TOKENS, GRADUATION_ETH, chartCoordinates, curveAt, priceMultiple } from "./curve";

describe("landing curve", () => {
  it("sells exactly the curve allocation at graduation", () => {
    expect(curveAt(GRADUATION_ETH).tokensSold).toBeCloseTo(CURVE_TOKENS, 0);
    expect(curveAt(0).tokensSold).toBe(0);
  });

  it("ends at the 16x price multiple implied by x0 = 1.4 ETH", () => {
    expect(priceMultiple(curveAt(GRADUATION_ETH))).toBeCloseTo(16, 6);
  });

  it("clamps outside the curve range and maps endpoints to the chart corners", () => {
    expect(curveAt(-1).ethRaised).toBe(0);
    expect(curveAt(99).progress).toBe(1);
    expect(chartCoordinates(curveAt(0), 100, 50)).toEqual([0, 50]);
    const [x, y] = chartCoordinates(curveAt(GRADUATION_ETH), 100, 50);
    expect(x).toBeCloseTo(100, 6);
    expect(y).toBeCloseTo(0, 6);
  });
});
