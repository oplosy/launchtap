import { describe, expect, it } from "vitest";
import {
  formatBaseUnits,
  formatCanonicalBaseUnits,
  formatDisplayAmount,
  formatSignificantPrice,
  formatUsd,
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

describe("formatUsd", () => {
  it("formats values of at least one dollar with two decimals and separators", () => {
    expect(formatUsd(10n ** 18n, 18, "3456.12")).toBe("$3,456.12");
    expect(formatUsd(10n ** 18n, 18, "3000")).toBe("$3,000.00");
  });
  it("keeps four significant digits below one dollar", () => {
    expect(formatUsd(1n, 18, "3456.12")).toBe("$0.000000000000003456");
    expect(formatUsd(10n ** 17n, 18, "5")).toBe("$0.5000");
  });
  it("is exact for very large amounts", () => {
    expect(formatUsd(10n ** 27n, 18, "3456.123456789012345678")).toBe("$3,456,123,456,789.01");
  });
  it("rounds half up", () => {
    expect(formatUsd(10n ** 18n, 18, "1.005")).toBe("$1.01");
    expect(formatUsd(10n ** 18n, 18, "1.004")).toBe("$1.00");
    expect(formatUsd(10n ** 18n, 18, "0.99996")).toBe("$1.00");
    expect(formatUsd(10n ** 18n, 18, "0.12345")).toBe("$0.1235");
  });
  it("renders zero and rejects bad input", () => {
    expect(formatUsd(0n, 18, "3456.12")).toBe("$0.00");
    expect(() => formatUsd(1n, 18, "abc")).toThrow();
    expect(() => formatUsd(-1n, 18, "1")).toThrow();
  });
});
