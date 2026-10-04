import { describe, expect, it } from "vitest";
import { configurationDrift, publicKeys } from "./container-preflight.mjs";

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
