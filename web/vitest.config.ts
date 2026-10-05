import { defineConfig } from "vitest/config";
import { fileURLToPath } from "node:url";

export default defineConfig({
  resolve: { alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) } },
  test: {
    environment: "node",
    include: [
      "src/**/*.test.ts",
      "scripts/check-budgets.test.mjs",
      "scripts/validate-release.test.mjs",
      "scripts/container-preflight.test.mjs",
      "scripts/verify-release-command.test.mjs",
    ],
  },
});
