import { NextResponse, type NextRequest } from "next/server";
import { contentSecurityPolicy } from "@/security/headers";

/**
 * The public endpoints the policy allows, read as literal `process.env.NEXT_PUBLIC_*` accesses so
 * the build inlines the reviewed values. The web container's runtime environment does not carry
 * them; passing `process.env` whole would read that runtime environment and drop the API and RPC
 * origins from the policy.
 */
const policyEnv = {
  NEXT_PUBLIC_API_BASE_URL: process.env.NEXT_PUBLIC_API_BASE_URL,
  NEXT_PUBLIC_RPC_URL: process.env.NEXT_PUBLIC_RPC_URL,
  NEXT_PUBLIC_E2E_FIXTURE: process.env.NEXT_PUBLIC_E2E_FIXTURE,
  NEXT_PUBLIC_TASK6_ANVIL_API_URL: process.env.NEXT_PUBLIC_TASK6_ANVIL_API_URL,
  NEXT_PUBLIC_TASK6_ANVIL_RPC_URL: process.env.NEXT_PUBLIC_TASK6_ANVIL_RPC_URL,
};

/** Issue a fresh script nonce per document request; Next.js applies it to its own scripts. */
export function proxy(request: NextRequest) {
  const nonce = btoa(crypto.randomUUID());
  const policy = contentSecurityPolicy({ ...policyEnv, NODE_ENV: process.env.NODE_ENV }, nonce);
  const requestHeaders = new Headers(request.headers);
  requestHeaders.set("x-nonce", nonce);
  requestHeaders.set("Content-Security-Policy", policy);
  const response = NextResponse.next({ request: { headers: requestHeaders } });
  response.headers.set("Content-Security-Policy", policy);
  return response;
}

export const config = {
  matcher: [
    {
      source: "/((?!_next/static|_next/image|favicon.ico).*)",
      missing: [
        { type: "header", key: "next-router-prefetch" },
        { type: "header", key: "purpose", value: "prefetch" },
      ],
    },
  ],
};
