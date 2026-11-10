import { describe, expect, it } from "vitest";
import {
  MAX_PUBLIC_WALLETS,
  nextChangeLabel,
  normalizeUsername,
  USERNAME_PATTERN,
  usernameHint,
} from "./public-profile-model";

describe("public profile model", () => {
  it("pins the spec values", () => {
    expect(MAX_PUBLIC_WALLETS).toBe(5);
    expect(USERNAME_PATTERN.test("abc")).toBe(true);
    expect(USERNAME_PATTERN.test("a".repeat(21))).toBe(false);
  });

  it("normalizes like the server", () => {
    expect(normalizeUsername(" Alice ")).toBe("alice");
  });

  it("hints format problems only", () => {
    expect(usernameHint("")).toBeNull();
    expect(usernameHint("Alice_1")).toBeNull();
    expect(usernameHint("admin")).toBeNull(); // reserved is a server answer
    expect(usernameHint("ab")).not.toBeNull();
    expect(usernameHint("al-ice")).not.toBeNull();
    expect(usernameHint("0xabc")).toContain("0x");
  });

  it("labels the next change date", () => {
    const now = new Date("2026-11-09T12:00:00Z");
    expect(nextChangeLabel(null, now)).toBeNull();
    expect(nextChangeLabel("2026-11-01T00:00:00Z", now)).toBeNull();
    expect(nextChangeLabel("2026-11-16T12:00:00.000000Z", now)).toBe(
      "You can change it again on 16 Nov 2026",
    );
  });
});
