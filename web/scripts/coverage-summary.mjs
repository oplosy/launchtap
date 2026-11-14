// Prints the vitest v8 coverage totals as a Markdown table for the CI job summary.
import { readFileSync } from "node:fs";

const path = process.argv[2] ?? "coverage/coverage-summary.json";
const { total } = JSON.parse(readFileSync(path, "utf8"));
const rows = ["lines", "statements", "functions", "branches"].map(
  (metric) =>
    `| ${metric} | ${total[metric].pct}% | ${total[metric].covered}/${total[metric].total} |`,
);
console.log(
  [
    "### Web unit test coverage",
    "",
    "| Metric | Covered | Count |",
    "|---|---:|---:|",
    ...rows,
  ].join("\n"),
);
