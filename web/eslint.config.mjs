import nextVitals from "eslint-config-next/core-web-vitals";
import nextTypescript from "eslint-config-next/typescript";

const eslintConfig = [
  ...nextVitals,
  ...nextTypescript,
  {
    // Keep source files reviewable: split a module before it passes ~400 lines of code
    // (docs/quality-scorecard.md, Maintainability). Tests and generated code are exempt.
    files: ["**/*.{ts,tsx}"],
    ignores: ["**/*.test.{ts,tsx}", "e2e/**", "**/generated.ts"],
    rules: {
      "max-lines": ["error", { max: 400, skipBlankLines: true, skipComments: true }],
    },
  },
];
export default eslintConfig;
