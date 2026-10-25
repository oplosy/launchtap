import { describe, expect, it } from "vitest";
import {
  formatBaseUnits,
  formatCanonicalBaseUnits,
  formatDisplayAmount,
  formatSignificantPrice,
  parseBaseUnits,
  parseCanonicalBaseUnits,
  parseDecimal,
  wadToBoundedNumber,
} from "./amounts";

describe("decimal and bigint amounts", () => {
  it("round trips exact boundaries without floating point", () => {
    expect(parseDecimal("1.000000000000000001")).toBe(1000000000000000001n);
    expect(formatBaseUnits(1000000000000000001n)).toBe("1.000000000000000001");
    expect(formatDisplayAmount(1234567890000000000000n)).toBe("1,234.5678");
  });
  it("rejects malformed and over-precise values", () => {
    expect(() => parseDecimal("1e3")).toThrow();
    expect(() => parseDecimal("1.001", 2)).toThrow();
    expect(() => parseBaseUnits("1.2")).toThrow();
    expect(() => parseDecimal("-1")).toThrow();
  });
  it("scales canonical WAD values exactly before bounded chart conversion", () => {
    expect(parseCanonicalBaseUnits("0")).toBe(0n);
    expect(formatCanonicalBaseUnits("1", 18)).toBe("0.000000000000000001");
    expect(formatCanonicalBaseUnits("1000000000000000001", 18)).toBe("1.000000000000000001");
    expect(wadToBoundedNumber("1000000000000000001")).toBeCloseTo(1.000000000000000001);
    expect(wadToBoundedNumber("1000000000000000000000000000000000000000000000")).toBeNull();
  });
  it("fails closed for whitespace, signs, hex, and noncanonical quantities", () => {
    for (const value of ["", " ", "01", "+1", "-1", "0x1", "1.0", "1e3"])
      expect(formatCanonicalBaseUnits(value)).toBeNull();
    for (const value of ["", " ", "01", "+1", "-1", "0x1", "1.0", "1e3"])
      expect(wadToBoundedNumber(value)).toBeNull();
    expect(wadToBoundedNumber("1", Number.NaN)).toBeNull();
    expect(wadToBoundedNumber("1", -1)).toBeNull();
  });
});

describe("formatSignificantPrice", () => {
  it("keeps four significant digits for sub-unit prices and rejects non-canonical input", () => {
    expect(formatSignificantPrice("12345678901")).toBe("0.00000001234");
    expect(formatSignificantPrice("1")).toBe("0.000000000000000001");
    expect(formatSignificantPrice("1500000000000000000")).toBe("1.5");
    expect(formatSignificantPrice("0")).toBe("0");
    expect(formatSignificantPrice("01")).toBeNull();
    expect(formatSignificantPrice("-1")).toBeNull();
  });
});
