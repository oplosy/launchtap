#!/usr/bin/env node
// Runtime dependency audit gate. Fails on any high or critical advisory in production
// dependencies unless it is listed in audit-allowlist.json with a reason and an unexpired
// review date. Low and moderate advisories are reported but never fail the gate.
import { spawnSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const BLOCKING = new Set(["high", "critical"]);
const ADVISORY_ID = /^GHSA-[a-z0-9]{4}-[a-z0-9]{4}-[a-z0-9]{4}$/;
const DATE = /^\d{4}-\d{2}-\d{2}$/;

/** Every advisory npm reports directly against a package, keyed by GHSA id. */
export function collectAdvisories(report) {
  const advisories = new Map();
  for (const vulnerability of Object.values(report?.vulnerabilities ?? {})) {
    for (const via of vulnerability.via ?? []) {
      if (typeof via !== "object" || via === null || typeof via.url !== "string") continue;
      const id = via.url.split("/").pop();
      const existing = advisories.get(id);
      const packages = new Set(existing?.packages ?? []);
      packages.add(via.name ?? vulnerability.name);
      advisories.set(id, {
        id,
        severity: via.severity,
        title: via.title ?? "",
        packages: [...packages].sort(),
      });
    }
  }
  return [...advisories.values()].sort((a, b) => a.id.localeCompare(b.id));
}

export function validateAllowlist(entries) {
  const problems = [];
  if (!Array.isArray(entries)) return ["audit allowlist must be a JSON array"];
  const seen = new Set();
  for (const [index, entry] of entries.entries()) {
    const where = `allowlist entry ${index}`;
    if (!ADVISORY_ID.test(entry?.id ?? "")) problems.push(`${where} has an invalid GHSA id`);
    else if (seen.has(entry.id)) problems.push(`${where} duplicates ${entry.id}`);
    else seen.add(entry.id);
    if (typeof entry?.reason !== "string" || entry.reason.trim().length < 20)
      problems.push(`${where} needs a reason of at least 20 characters`);
    if (!DATE.test(entry?.reviewBy ?? "") || Number.isNaN(Date.parse(entry.reviewBy)))
      problems.push(`${where} needs a reviewBy date (YYYY-MM-DD)`);
  }
  return problems;
}

/** Returns the failures that should block CI and the notes that are only reported. */
export function evaluateAudit(advisories, allowlist, today) {
  const failures = [];
  const notes = [];
  const allowed = new Map(allowlist.map((entry) => [entry.id, entry]));
  for (const advisory of advisories) {
    if (!BLOCKING.has(advisory.severity)) continue;
    const entry = allowed.get(advisory.id);
    const label = `${advisory.severity} ${advisory.id} in ${advisory.packages.join(", ")}: ${advisory.title}`;
    if (!entry) failures.push(`unreviewed ${label}`);
    else if (entry.reviewBy < today)
      failures.push(`allowlist review expired on ${entry.reviewBy}: ${label}`);
  }
  const present = new Set(advisories.map((advisory) => advisory.id));
  for (const entry of allowlist) {
    if (!present.has(entry.id))
      notes.push(`allowlist entry ${entry.id} no longer matches an advisory; remove it`);
  }
  return { failures, notes };
}

function main() {
  const webRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
  const args = ["audit", "--omit=dev", "--json"];
  // A user-level allow-scripts setting is exported to `npm run` children and makes a nested
  // project-scoped npm refuse to start; it is irrelevant to a read-only audit.
  const env = { ...process.env };
  delete env.npm_config_allow_scripts;
  const options = { cwd: webRoot, encoding: "utf8", env, maxBuffer: 64 * 1024 * 1024 };
  // Under `npm run`, reuse the running npm's CLI through node so no shell is needed.
  const result = process.env.npm_execpath
    ? spawnSync(process.execPath, [process.env.npm_execpath, ...args], options)
    : spawnSync(process.platform === "win32" ? "npm.cmd" : "npm", args, {
        ...options,
        shell: process.platform === "win32",
      });
  let report;
  try {
    report = JSON.parse(result.stdout);
  } catch {
    console.error("npm audit did not return JSON:", result.stderr || result.stdout);
    process.exit(2);
  }
  if (report.error) {
    console.error("npm audit failed:", report.error.summary ?? report.error);
    process.exit(2);
  }
  const allowlist = JSON.parse(fs.readFileSync(path.join(webRoot, "audit-allowlist.json"), "utf8"));
  const problems = validateAllowlist(allowlist);
  if (problems.length > 0) {
    problems.forEach((problem) => console.error(problem));
    process.exit(1);
  }
  const advisories = collectAdvisories(report);
  const today = new Date().toISOString().slice(0, 10);
  const { failures, notes } = evaluateAudit(advisories, allowlist, today);
  const counts = report.metadata?.vulnerabilities ?? {};
  console.log(`runtime audit: ${JSON.stringify(counts)}`);
  notes.forEach((note) => console.log(note));
  if (failures.length > 0) {
    failures.forEach((failure) => console.error(failure));
    process.exit(1);
  }
  console.log("runtime audit: no unreviewed high or critical advisories");
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) main();
