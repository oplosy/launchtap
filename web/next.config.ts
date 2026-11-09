import type { NextConfig } from "next";
import { cacheHeaders, securityHeaders } from "./src/security/headers";

const nextConfig: NextConfig = {
  output: process.env.CONTAINER_BUILD === "1" ? "standalone" : undefined,
  typedRoutes: true,
  // The web container has a read-only root filesystem: keep ISR entries (share cards) in memory
  // instead of flushing them to .next/server/app.
  experimental: { isrFlushToDisk: false },
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
