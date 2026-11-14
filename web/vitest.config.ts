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
    },
  },
});
