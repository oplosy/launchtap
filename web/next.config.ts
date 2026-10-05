import type { NextConfig } from "next";
import { cacheHeaders, securityHeaders } from "./src/security/headers";

const nextConfig: NextConfig = {
  output: process.env.CONTAINER_BUILD === "1" ? "standalone" : undefined,
  typedRoutes: true,
  async headers() {
    return [
      { source: "/(.*)", headers: securityHeaders() },
      // Leave content-hashed Next assets on the framework's immutable cache path.
      { source: "/((?!_next/static|_next/image|favicon.ico).*)", headers: cacheHeaders },
    ];
  },
};
export default nextConfig;
