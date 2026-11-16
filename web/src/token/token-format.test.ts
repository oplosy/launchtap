import { describe, expect, it } from "vitest";
import { detailsEditedLabel } from "./token-format";

describe("detailsEditedLabel", () => {
  it("formats the edit date in UTC", () => {
    expect(detailsEditedLabel("2026-11-16T23:30:00Z")).toBe("Details edited 16 Nov 2026");
  });

  it("returns null when the details were never edited or the value is invalid", () => {
    expect(detailsEditedLabel(null)).toBeNull();
    expect(detailsEditedLabel(undefined)).toBeNull();
    expect(detailsEditedLabel("not-a-date")).toBeNull();
  });
});
