import { describe, expect, it } from "vitest";
import { apiInternalErrors, configurationDrift, publicKeys } from "./container-preflight.mjs";

describe("container promotion", () => {
  const built = Object.fromEntries(publicKeys.map((key) => [key, `built-${key}`]));

  it("accepts unchanged baked public configuration", () => {
    expect(configurationDrift(built, { ...built, DATABASE_URL: "server-only" })).toEqual([]);
  });

  it("rejects changing a public endpoint or deployment without rebuilding", () => {
    const errors = configurationDrift(built, {
      ...built,
      NEXT_PUBLIC_API_BASE_URL: "https://other-api.example",
      NEXT_PUBLIC_DEPLOYMENT_ID: "other-deployment",
    });
    expect(errors).toHaveLength(2);
    expect(errors.join("\n")).toContain("NEXT_PUBLIC_API_BASE_URL differs");
    expect(errors.join("\n")).toContain("NEXT_PUBLIC_DEPLOYMENT_ID differs");
  });
});

describe("API_INTERNAL_BASE_URL", () => {
  it("accepts an unset value and http(s) URLs", () => {
    expect(apiInternalErrors({})).toEqual([]);
    expect(apiInternalErrors({ API_INTERNAL_BASE_URL: "http://api:8080" })).toEqual([]);
    expect(apiInternalErrors({ API_INTERNAL_BASE_URL: "https://api.example" })).toEqual([]);
  });

  it("rejects an unset value when the public API base is loopback", () => {
    for (const base of ["http://localhost:8080", "http://127.0.0.1:8080", "http://[::1]:8080"])
      expect(apiInternalErrors({ NEXT_PUBLIC_API_BASE_URL: base })).toHaveLength(1);
    expect(apiInternalErrors({ NEXT_PUBLIC_API_BASE_URL: "https://api.example" })).toEqual([]);
    expect(
      apiInternalErrors({
        NEXT_PUBLIC_API_BASE_URL: "http://localhost:8080",
        API_INTERNAL_BASE_URL: "http://api:8080",
      }),
    ).toEqual([]);
  });

  it("rejects a non-http(s) API_INTERNAL_BASE_URL", () => {
    for (const value of ["ftp://api:21", "javascript:alert(1)", "file:///etc/passwd", "not a url"])
      expect(apiInternalErrors({ API_INTERNAL_BASE_URL: value })).toHaveLength(1);
    expect(apiInternalErrors({ API_INTERNAL_BASE_URL: "http://u:p@api:8080" })).toHaveLength(1);
  });
});
