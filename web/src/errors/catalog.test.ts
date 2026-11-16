import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { browserAbis } from "@/contracts/generated";
import {
  apiMessages,
  contextMessages,
  contractMessages,
  panicMessages,
  walletMessages,
} from "./catalog";

/**
 * Drift guard (docs/specs/2026-11-16-user-error-messages-preflight.md, decision 8): every problem
 * code the API can send and every custom error the reviewed contracts can revert with has its own
 * sentence, so no failure reaches a user as a raw code or the generic fallback.
 */
const apiserverDir = join(__dirname, "..", "..", "..", "backend", "internal", "apiserver");

function backendProblemCodes() {
  const codes = new Set<string>();
  const patterns = [
    /apiProblem\(\s*[^,]+,\s*"([a-z0-9_]+)"/g,
    /apiRequestProblem\(\s*[^,]+,\s*[^,]+,\s*"([a-z0-9_]+)"/g,
    /\bproblem\(\s*w\s*,\s*[^,]+,\s*"([a-z0-9_]+)"/g,
  ];
  for (const file of readdirSync(apiserverDir)) {
    if (!file.endsWith(".go") || file.endsWith("_test.go")) continue;
    const source = readFileSync(join(apiserverDir, file), "utf8");
    for (const pattern of patterns)
      for (const match of source.matchAll(pattern)) codes.add(match[1]!);
    if (file === "problem_codes.go")
      // `"http_" + status` is the catch-all for statuses huma never sends; not a fixed code.
      for (const match of source.matchAll(/return "([a-z0-9_]*[a-z0-9])"/g)) codes.add(match[1]!);
  }
  return codes;
}

describe("error catalog", () => {
  it("has a message for every problem code the API sends", () => {
    const codes = backendProblemCodes();
    expect(codes.size).toBeGreaterThan(40);
    const missing = [...codes].filter((code) => !apiMessages[code]);
    expect(missing).toEqual([]);
  });

  it("has a message for every custom error in the reviewed contract ABIs", () => {
    const names = new Set<string>();
    for (const abi of Object.values(browserAbis) as unknown as readonly (readonly {
      type: string;
      name?: string;
    }[])[])
      for (const item of abi) if (item.type === "error" && item.name) names.add(item.name);
    expect(names.size).toBeGreaterThan(50);
    const missing = [...names].filter((name) => !contractMessages[name]);
    expect(missing).toEqual([]);
  });

  it("only uses complete sentences", () => {
    const sentences = [
      ...Object.values(apiMessages),
      ...Object.values(contractMessages),
      ...Object.values(panicMessages),
      ...Object.values(walletMessages),
      ...Object.values(contextMessages).flatMap((words) => Object.values(words)),
    ];
    for (const sentence of sentences) {
      expect(sentence, sentence).toMatch(/^[A-Z0-9"']/);
      expect(sentence, sentence).toMatch(/[.!?]$/);
    }
  });

  it("only overrides codes the API actually sends in a context", () => {
    const known = new Set([...Object.keys(apiMessages), "fallback"]);
    for (const [context, words] of Object.entries(contextMessages))
      for (const code of Object.keys(words))
        expect(known.has(code), `${context}.${code}`).toBe(true);
  });
});
