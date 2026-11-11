// Build-time alias (next.config.ts `turbopack.resolveAlias`): the test session provider in the
// E2E fixture build, an inert stub in every other build. Typed as the real module.
declare module "launchpad-e2e-session" {
  export { E2ESessionProvider } from "@/e2e/session-provider";
}
