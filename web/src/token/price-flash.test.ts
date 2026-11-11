import { describe, expect, it } from "vitest";
import { priceDirection } from "./price-flash";

describe("priceDirection", () => {
  it("compares base-unit strings exactly, beyond float precision", () => {
    expect(priceDirection("1000000000000000000", "1000000000000000001")).toBe("up");
    expect(priceDirection("2000000000000000000", "1999999999999999999")).toBe("down");
  });

  it("returns null when the price is unchanged or unreadable", () => {
    expect(priceDirection("5", "5")).toBeNull();
    expect(priceDirection("", "5")).toBeNull();
    expect(priceDirection("5", "1.5")).toBeNull();
    expect(priceDirection("-1", "5")).toBeNull();
  });
});
