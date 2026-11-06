// Runs the live-strip specs and the release-hardening budgets with a READY configuration.
// The fixture build only needs well-formed dummy values: every API call is mocked with
// page.route, so no Anvil node, network or secret is involved. The fail-closed e2e step
// (`npm run test:e2e`) is unchanged and keeps running without these values.
import { spawnSync } from "node:child_process";

const env = {
  ...process.env,
  TASK6_ANVIL_FACTORY: "0x0000000000000000000000000000000000000011",
  TASK6_ANVIL_WETH: "0x0000000000000000000000000000000000000012",
  TASK6_ANVIL_ROUTER: "0x0000000000000000000000000000000000000013",
  TASK6_ANVIL_UNISWAP_FACTORY: "0x0000000000000000000000000000000000000014",
  TASK6_ANVIL_RPC_URL: "http://127.0.0.1:18545",
  TASK6_ANVIL_API_URL: "http://127.0.0.1:18080",
};

const result = spawnSync(
  process.platform === "win32" ? "npx.cmd" : "npx",
  [
    "playwright",
    "test",
    "live-strip",
    "notifications",
    "release-hardening",
    // The /create footer check is a fail-closed-configuration check (ready config lays /create out differently).
    "--grep-invert",
    "mobile footer",
    ...process.argv.slice(2),
  ],
  { stdio: "inherit", env, shell: process.platform === "win32" },
);
process.exit(result.status ?? 1);
