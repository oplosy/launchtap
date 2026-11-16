import { publicConfiguration } from "./public";

export type ReleaseValidation = { ok: boolean; errors: string[] };

// A fixture word must start a token ("testnet", "e2e-rpc"), so random IDs that merely contain
// the letters ("cmabe2e9", "latest") are not rejected. Mirrors scripts/validate-release.mjs.
const forbiddenProductionValue =
  /(?:^|[^a-z0-9])(?:task6|e2e|anvil|localhost|test|mock|fixture)|127\.0\.0\.1/i;

export function validateReleaseEnvironment(
  env: Record<string, string | undefined>,
  mode: "production" | "anvil" = "production",
): ReleaseValidation {
  const errors: string[] = [];
  const configuration = publicConfiguration(env);
  if (configuration.status !== "ready")
    errors.push("reviewed public deployment configuration is incomplete");
  if (!configuration.deployment?.enabled)
    errors.push("deployment is not enabled in the reviewed manifest");
  if (mode === "production") {
    for (const key of [
      "NEXT_PUBLIC_PRIVY_APP_ID",
      "NEXT_PUBLIC_DEPLOYMENT_ID",
      "NEXT_PUBLIC_API_BASE_URL",
      "NEXT_PUBLIC_RPC_URL",
      "NEXT_PUBLIC_WEB_ORIGIN",
    ]) {
      const value = env[key]?.trim();
      if (value && forbiddenProductionValue.test(value))
        errors.push(`${key} contains a fixture or test value`);
    }
    if (env.NEXT_PUBLIC_E2E_FIXTURE === "1")
      errors.push("E2E fixture configuration cannot ship to production");
  } else if (env.NEXT_PUBLIC_E2E_FIXTURE !== "1") {
    errors.push("Anvil validation requires NEXT_PUBLIC_E2E_FIXTURE=1");
  }
  return { ok: errors.length === 0, errors };
}
