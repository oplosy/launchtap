import { describe, expect, it } from "vitest";
import { collectAdvisories, evaluateAudit, validateAllowlist } from "./audit-runtime.mjs";

const report = {
  vulnerabilities: {
    zod: {
      name: "zod",
      via: [
        {
          name: "zod",
          severity: "high",
          title: "memory exhaustion",
          url: "https://github.com/advisories/GHSA-mrhx-c7xc-m3w5",
        },
      ],
    },
    abitype: { name: "abitype", via: ["zod"] },
    uuid: {
      name: "uuid",
      via: [
        {
          name: "uuid",
          severity: "moderate",
          title: "bounds",
          url: "https://github.com/advisories/GHSA-w5hq-g745-h8pq",
        },
      ],
    },
  },
};

const entry = {
  id: "GHSA-mrhx-c7xc-m3w5",
  reason: "Only reachable through wallet SDK schemas fed by trusted constants.",
  reviewBy: "2026-12-31",
};

describe("runtime audit gate", () => {
  it("collects direct advisories once and ignores transitive references", () => {
    expect(collectAdvisories(report).map((advisory) => advisory.id)).toEqual([
      "GHSA-mrhx-c7xc-m3w5",
      "GHSA-w5hq-g745-h8pq",
    ]);
  });

  it("fails on an unreviewed high advisory but never on a moderate one", () => {
    const { failures } = evaluateAudit(collectAdvisories(report), [], "2026-11-06");
    expect(failures).toHaveLength(1);
    expect(failures[0]).toContain("unreviewed high GHSA-mrhx-c7xc-m3w5");
  });

  it("accepts an allowlisted advisory until its review date passes", () => {
    const advisories = collectAdvisories(report);
    expect(evaluateAudit(advisories, [entry], "2026-12-31").failures).toEqual([]);
    expect(evaluateAudit(advisories, [entry], "2027-01-01").failures[0]).toContain(
      "allowlist review expired on 2026-12-31",
    );
  });

  it("reports allowlist entries that no longer match an advisory", () => {
    const { notes } = evaluateAudit([], [entry], "2026-11-06");
    expect(notes).toEqual([
      "allowlist entry GHSA-mrhx-c7xc-m3w5 no longer matches an advisory; remove it",
    ]);
  });

  it("rejects malformed, duplicate or unjustified allowlist entries", () => {
    expect(validateAllowlist([entry])).toEqual([]);
    expect(validateAllowlist({})).toEqual(["audit allowlist must be a JSON array"]);
    expect(
      validateAllowlist([entry, entry, { id: "CVE-1", reason: "short", reviewBy: "soon" }]),
    ).toEqual([
      "allowlist entry 1 duplicates GHSA-mrhx-c7xc-m3w5",
      "allowlist entry 2 has an invalid GHSA id",
      "allowlist entry 2 needs a reason of at least 20 characters",
      "allowlist entry 2 needs a reviewBy date (YYYY-MM-DD)",
    ]);
  });
});
