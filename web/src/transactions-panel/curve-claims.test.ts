import { describe, expect, it } from "vitest";
import { showClaim } from "./curve-claims";

describe("showClaim", () => {
  it("hides a claim with nothing to claim and no claim under way", () => {
    expect(showClaim(null, "disconnected")).toBe(false);
    expect(showClaim(0n, "validating")).toBe(false);
  });
  it("shows a positive balance, and keeps showing a claim's progress after it empties", () => {
    expect(showClaim(1n, "disconnected")).toBe(true);
    expect(showClaim(0n, "submitted")).toBe(true);
    expect(showClaim(0n, "rejected-signature")).toBe(true);
  });
});
