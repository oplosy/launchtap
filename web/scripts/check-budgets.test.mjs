import fs from "node:fs";
import os from "node:os";
import path from "node:path";

import { describe, expect, it } from "vitest";
import budgetMatrix from "../performance-budgets.json" with { type: "json" };
import {
  evaluateBuildBudget,
  maxInitialJavaScriptBytes,
  scanBundleText,
  validatePerformanceBudgetMatrix,
} from "./check-budgets.mjs";

describe("release bundle and performance budgets", () => {
  it("requires every profile and core route, including populated token detail", () => {
    expect(validatePerformanceBudgetMatrix()).toEqual([]);
    expect(
      validatePerformanceBudgetMatrix({
        version: 2,
        profiles: {},
        routeClasses: {
          "read-only": { routes: ["/"], transferredBytes: 1, initialJavaScriptBytes: 1 },
        },
      }),
    ).toEqual(
      expect.arrayContaining([
        "performance budget is missing mobile profile",
        "performance budget is missing the wallet route class",
        "performance budget is missing the populated token fixture route",
      ]),
    );
  });

  it("keeps wallet routes out of the read-only class and its budget below wallet routes", () => {
    const matrix = structuredClone(budgetMatrix);
    matrix.routeClasses["read-only"].routes.push("/create");
    matrix.routeClasses["read-only"].initialJavaScriptBytes =
      matrix.routeClasses.wallet.initialJavaScriptBytes;
    expect(validatePerformanceBudgetMatrix(matrix)).toEqual(
      expect.arrayContaining([
        "/create is budgeted by both read-only and wallet",
        "wallet route /create cannot use the read-only budget",
        "read-only initial JavaScript budget must be below the wallet budget",
      ]),
    );
  });

  it("rejects an initial JavaScript overage", () => {
    const buildRoot = fs.mkdtempSync(path.join(os.tmpdir(), "launchpad-budget-"));
    try {
      const chunks = path.join(buildRoot, "static", "chunks");
      fs.mkdirSync(chunks, { recursive: true });
      fs.writeFileSync(path.join(chunks, "main.js"), Buffer.alloc(maxInitialJavaScriptBytes + 1));
      expect(evaluateBuildBudget(buildRoot).violations).toEqual([
        `initial JavaScript ${maxInitialJavaScriptBytes + 1} > ${maxInitialJavaScriptBytes} bytes`,
      ]);
    } finally {
      fs.rmSync(buildRoot, { recursive: true, force: true });
    }
  });

  it("allows transaction hashes but rejects secret assignments and known Anvil values", () => {
    const transactionHash = `0x${"12ab".repeat(16)}`;
    expect(scanBundleText(`const txHash = "${transactionHash}";`, "transaction.js")).toEqual([]);

    const key = `0x${"ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"}`;
    const findings = scanBundleText(
      `const privateKey = "${transactionHash}"; const accountSecret = "${"ab12".repeat(16)}"; ${key} 0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266 0x1111111111111111111111111111111111111111`,
      "fixture.js",
    );
    expect(findings.some((value) => value.includes("private-key"))).toBe(true);
    expect(findings.some((value) => value.includes("known Anvil address 0xf39"))).toBe(true);
    expect(findings.some((value) => value.includes("unreviewed address"))).toBe(true);
  });

  it("rejects raw known Anvil addresses even in otherwise reviewed-looking contexts", () => {
    const findings = scanBundleText(
      'const network = "anvil"; const deployer = "0x70997970c51812dc3a010c7d01b50e0d17dc79c8";',
      "anvil.js",
    );
    expect(findings).toEqual([
      expect.stringContaining("known Anvil address 0x70997970c51812dc3a010c7d01b50e0d17dc79c8"),
    ]);
  });

  it("allows reviewed deployment addresses and contiguous bytecode blobs", () => {
    const reviewed = "0x0Bd7D308f8E1639FAb988df18A8011f41EAcAD73";
    const bytecode = `0x${"ab".repeat(80)}`;
    expect(scanBundleText(`${reviewed} ${bytecode}`, "generated.js")).toEqual([]);
  });
});
