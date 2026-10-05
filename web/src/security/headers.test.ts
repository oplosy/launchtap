import { describe, expect, it } from "vitest";
import { contentSecurityPolicy, securityHeaders } from "./headers";

describe("security headers", () => {
  it("uses only reviewed endpoint origins and no wildcard https connect source", () => {
    const policy = contentSecurityPolicy({
      NEXT_PUBLIC_API_BASE_URL: "https://api.launchpad.example/v1",
      NEXT_PUBLIC_RPC_URL: "https://rpc.launchpad.example",
    });
    expect(policy).toContain("https://api.launchpad.example");
    expect(policy).toContain("https://rpc.launchpad.example");
    expect(policy).not.toMatch(/connect-src 'self' https:(?:\s|;)/);
    expect(policy).toContain("img-src 'self' data: blob: https:");
  });

  it("rejects credentials and query strings in CSP origins", () => {
    const policy = contentSecurityPolicy({
      NEXT_PUBLIC_API_BASE_URL: "https://user:secret@example.test?token=bad",
    });
    expect(policy).not.toContain("example.test");
  });

  it("allows only explicit Anvil origins in the E2E fixture policy", () => {
    const fixturePolicy = contentSecurityPolicy({
      NEXT_PUBLIC_E2E_FIXTURE: "1",
      NEXT_PUBLIC_TASK6_ANVIL_API_URL: "http://127.0.0.1:18080",
      NEXT_PUBLIC_TASK6_ANVIL_RPC_URL: "http://127.0.0.1:18545",
    });
    expect(fixturePolicy).toContain("http://127.0.0.1:18080");
    expect(fixturePolicy).toContain("http://127.0.0.1:18545");
    const productionPolicy = contentSecurityPolicy({
      NEXT_PUBLIC_TASK6_ANVIL_API_URL: "http://127.0.0.1:18080",
      NEXT_PUBLIC_TASK6_ANVIL_RPC_URL: "http://127.0.0.1:18545",
    });
    expect(productionPolicy).not.toContain("http://127.0.0.1:18080");
    expect(productionPolicy).not.toContain("http://127.0.0.1:18545");
  });

  it("includes browser hardening headers and leaves CSP to the per-request proxy", () => {
    const keys = securityHeaders().map((header) => header.key);
    expect(keys).toEqual(expect.arrayContaining(["Cross-Origin-Opener-Policy", "X-Frame-Options"]));
    expect(keys).not.toContain("Content-Security-Policy");
  });

  it("allows scripts only through the request nonce, never unsafe-inline", () => {
    const policy = contentSecurityPolicy({ NODE_ENV: "production" }, "abc123");
    expect(policy).toContain("script-src 'self' 'nonce-abc123' 'strict-dynamic';");
    expect(policy).not.toMatch(/script-src[^;]*unsafe-(inline|eval)/);
    expect(contentSecurityPolicy({ NODE_ENV: "development" }, "abc123")).toContain(
      "'strict-dynamic' 'unsafe-eval'",
    );
    expect(contentSecurityPolicy()).toContain("script-src 'self';");
  });
});
