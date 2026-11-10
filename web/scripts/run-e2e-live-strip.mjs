// Runs the live-strip specs and the release-hardening budgets with a READY configuration.
// The fixture build only needs well-formed dummy values: every API call is mocked with
// page.route, so no Anvil node, network or secret is involved. The fail-closed e2e step
// (`npm run test:e2e`) is unchanged and keeps running without these values.
import { spawnSync } from "node:child_process";
import { createRequire } from "node:module";

const env = {
  ...process.env,
  TASK6_ANVIL_FACTORY: "0x0000000000000000000000000000000000000011",
  TASK6_ANVIL_WETH: "0x0000000000000000000000000000000000000012",
  TASK6_ANVIL_ROUTER: "0x0000000000000000000000000000000000000013",
  TASK6_ANVIL_UNISWAP_FACTORY: "0x0000000000000000000000000000000000000014",
  TASK6_ANVIL_RPC_URL: "http://127.0.0.1:18545",
  TASK6_ANVIL_API_URL: "http://127.0.0.1:18080",
  // Without Privy the wallet menu offers the injected "Browser wallet" connector that the
  // notifications spec drives. Set explicitly so CI matches a developer's .env.local.
  NEXT_PUBLIC_LOCAL_DEV: "1",
};

// Run the Playwright CLI through this Node binary, without a shell: with `shell: true` on Windows
// cmd.exe re-splits the joined argument string, so a multi-word value (a --grep pattern) would
// break apart. No shell means every argument reaches Playwright exactly as written.
const playwrightCli = createRequire(import.meta.url).resolve("@playwright/test/cli");
const result = spawnSync(
  process.execPath,
  [
    playwrightCli,
    "test",
    "live-strip",
    "wallet-page",
    "notifications",
    "release-hardening",
    "token-comments",
    "public-profile",
    "forum",
    "signed-in",
    "explore",
    "home",
    "token-layout",
    ...process.argv.slice(2),
  ],
  { stdio: "inherit", env },
);
if (result.error) {
  console.error(`Playwright failed to start: ${result.error.message}`);
  process.exit(1);
}
process.exit(result.status ?? 1);
