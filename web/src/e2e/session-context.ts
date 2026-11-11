"use client";

import { createContext } from "react";

/**
 * The window property a browser test sets (before load) to sign the fixture build in. Honoured
 * only when `NEXT_PUBLIC_E2E_FIXTURE=1`, which a deployable build never sets
 * (`scripts/validate-release.mjs` rejects it).
 */
export const E2E_SESSION_KEY = "__e2eSession";

/** A fixed, test-only signed-in account. The tokens are opaque strings the mocked API accepts. */
export type E2ESession = {
  userId: string;
  email: string | null;
  accessToken: string;
  identityToken: string;
  linkedWallets: `0x${string}`[];
};

/**
 * Supplied only by the fixture-build session provider (`./session-provider`). Everywhere else it
 * stays null and account views read the Privy session.
 */
export const E2ESessionContext = createContext<E2ESession | null>(null);

const ADDRESS = /^0x[0-9a-fA-F]{40}$/;
const nonEmpty = (value: unknown): value is string =>
  typeof value === "string" && value.trim() !== "";

/** Validates what a test put on `window`; anything malformed means "no session". */
export function parseE2ESession(value: unknown): E2ESession | null {
  if (!value || typeof value !== "object") return null;
  const input = value as Record<string, unknown>;
  if (!nonEmpty(input.userId) || !nonEmpty(input.accessToken) || !nonEmpty(input.identityToken))
    return null;
  const wallets = input.linkedWallets ?? [];
  if (!Array.isArray(wallets) || !wallets.every((w) => typeof w === "string" && ADDRESS.test(w)))
    return null;
  return {
    userId: input.userId,
    email: nonEmpty(input.email) ? input.email : null,
    accessToken: input.accessToken,
    identityToken: input.identityToken,
    linkedWallets: wallets as `0x${string}`[],
  };
}
