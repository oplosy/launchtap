import { defineConfig } from "vitest/config";
import { fileURLToPath } from "node:url";

export default defineConfig({
  resolve: { alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) } },
  test: {
    environment: "node",
    include: [
      "src/**/*.test.ts",
      "src/**/*.test.tsx",
      "scripts/check-budgets.test.mjs",
      "scripts/contracts-model.test.mjs",
      "scripts/validate-release.test.mjs",
      "scripts/container-preflight.test.mjs",
      "scripts/verify-release-command.test.mjs",
      "scripts/audit-runtime.test.mjs",
    ],
    coverage: {
      provider: "v8",
      include: ["src/**/*.{ts,tsx}"],
      exclude: ["src/**/*.test.{ts,tsx}", "src/api/generated.ts"],
      reporter: ["text-summary", "json-summary"],
      reportsDirectory: "coverage",
      // Floors for the modules that build, sign and track transactions and that call the API
      // (each about two points under the measured value, 2026-11-14). `npm run test:coverage` in
      // the web CI job fails below them. Raise a floor when coverage rises; never lower one to
      // land a change. The panel views are covered by the browser e2e suites instead.
      thresholds: {
        "src/api/**": { lines: 94, statements: 90, functions: 91, branches: 80 },
        "src/transaction*.ts": { lines: 93, statements: 87, functions: 93, branches: 85 },
        "src/transactions-panel/{direct-write,reviewed-trade,launch-metadata,transaction-persistence,curve-trade-logic}.{ts,tsx}":
          { lines: 90, statements: 88, functions: 73, branches: 80 },
      },
    },
  },
});
