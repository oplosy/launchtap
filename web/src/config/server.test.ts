import { describe, expect, it } from "vitest";
import { apiInternalBaseUrl, webOrigin } from "./server";

describe("apiInternalBaseUrl", () => {
  it("prefers API_INTERNAL_BASE_URL over the public base", () => {
    expect(
      apiInternalBaseUrl({
        API_INTERNAL_BASE_URL: "http://api:8080/",
        NEXT_PUBLIC_API_BASE_URL: "https://api.example",
      }),
    ).toBe("http://api:8080");
  });

  it("falls back to NEXT_PUBLIC_API_BASE_URL", () => {
    expect(apiInternalBaseUrl({ NEXT_PUBLIC_API_BASE_URL: "https://api.example" })).toBe(
      "https://api.example",
    );
  });

  it("rejects ftp: and javascript:", () => {
    expect(() => apiInternalBaseUrl({ API_INTERNAL_BASE_URL: "ftp://api:21" })).toThrow(/http/);
    expect(() => apiInternalBaseUrl({ API_INTERNAL_BASE_URL: "javascript:alert(1)" })).toThrow();
    expect(() => apiInternalBaseUrl({ NEXT_PUBLIC_API_BASE_URL: "ftp://api.example" })).toThrow();
  });

  it("rejects credentials, query strings and a missing value", () => {
    expect(() => apiInternalBaseUrl({ API_INTERNAL_BASE_URL: "http://u:p@api:8080" })).toThrow();
    expect(() => apiInternalBaseUrl({ API_INTERNAL_BASE_URL: "http://api:8080/?k=v" })).toThrow();
    expect(() => apiInternalBaseUrl({})).toThrow(/required/);
  });
});

describe("webOrigin", () => {
  it("returns a configured origin and null otherwise", () => {
    expect(webOrigin({ NEXT_PUBLIC_WEB_ORIGIN: "https://launch.example/" })).toBe(
      "https://launch.example",
    );
    expect(webOrigin({ NEXT_PUBLIC_WEB_ORIGIN: "javascript:alert(1)" })).toBeNull();
    expect(webOrigin({})).toBeNull();
  });
});
