import type { PropsWithChildren } from "react";

/**
 * What `launchpad-e2e-session` resolves to in every build except the E2E fixture build
 * (`next.config.ts`), so the test session provider never ships in a deployable bundle.
 */
export function E2ESessionProvider({ children }: PropsWithChildren) {
  return children;
}
