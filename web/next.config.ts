import type { NextConfig } from "next";
import { cacheHeaders, securityHeaders } from "./src/security/headers";

const nextConfig: NextConfig = {
  output: process.env.CONTAINER_BUILD === "1" ? "standalone" : undefined,
  typedRoutes: true,
  // The web container has a read-only root filesystem: keep ISR entries (share cards) in memory
  // instead of flushing them to .next/server/app.
  experimental: { isrFlushToDisk: false },
  // The test session provider (signed-in browser e2e) exists only in the fixture build; every
  // other build resolves it to an inert stub so it never ships.
  turbopack: {
    resolveAlias: {
      "launchpad-e2e-session":
        process.env.NEXT_PUBLIC_E2E_FIXTURE === "1"
          ? "./src/e2e/session-provider.tsx"
          : "./src/e2e/session-provider.disabled.ts",
    },
  },
  async headers() {
    return [
      { source: "/(.*)", headers: securityHeaders() },
      // Leave content-hashed Next assets on the framework's immutable cache path.
      // Share-card images (token and wallet) set their own public cache policy (revalidate = 300,
      // or a short lifetime for a wallet generic card served during an API failure).
      {
        source:
          "/((?!_next/static|_next/image|favicon.ico|(?:token|wallet)/[^/]+/(?:opengraph|twitter)-image$).*)",
        headers: cacheHeaders,
      },
    ];
  },
};
export default nextConfig;
