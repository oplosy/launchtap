import type { NextConfig } from "next";
import { cacheHeaders, securityHeaders } from "./src/security/headers";

const nextConfig: NextConfig = {
  output: process.env.CONTAINER_BUILD === "1" ? "standalone" : undefined,
  typedRoutes: true,
  async headers() {
    return [
      { source: "/(.*)", headers: securityHeaders() },
      // Leave content-hashed Next assets on the framework's immutable cache path.
      // Share-card images set their own public cache policy (revalidate = 300).
      {
        source:
          "/((?!_next/static|_next/image|favicon.ico|token/[^/]+/(?:opengraph|twitter)-image$).*)",
        headers: cacheHeaders,
      },
    ];
  },
};
export default nextConfig;
