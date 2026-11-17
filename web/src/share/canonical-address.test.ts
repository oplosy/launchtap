import { describe, expect, it } from "vitest";
import { isCanonicalAddressSegment } from "./canonical-address";

describe("isCanonicalAddressSegment", () => {
  const lower = "0x5aaeb6053f3e94c9b9a09f33669435e7ef1beaed";
  const checksummed = "0x5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAed";

  it("accepts the lowercase and EIP-55 spellings", () => {
    expect(isCanonicalAddressSegment(lower)).toBe(true);
    expect(isCanonicalAddressSegment(checksummed)).toBe(true);
  });

  it("rejects other casings and malformed segments", () => {
    expect(isCanonicalAddressSegment(lower.toUpperCase().replace("0X", "0x"))).toBe(false);
    expect(isCanonicalAddressSegment("0x5AAeb6053f3e94c9b9a09f33669435e7ef1beaed")).toBe(false);
    expect(isCanonicalAddressSegment("not-an-address")).toBe(false);
  });
});
