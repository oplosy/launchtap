import fs from "node:fs";
import path from "node:path";
import { pathToFileURL } from "node:url";

import budgetMatrix from "../performance-budgets.json" with { type: "json" };

const root = path.resolve(process.cwd(), ".next");
export const maxInitialJavaScriptBytes = 1_800_000;
export const maxAllJavaScriptBytes = 8_000_000;
export const maxRouteManifestBytes = 500_000;
const addressPattern = /\b0x[0-9a-f]{40}\b/gi;
const privateKeyPattern = /(?<![0-9a-f])(?:0x)?[0-9a-f]{64}(?![0-9a-f])/gi;
const secretAssignmentPattern =
  /(?:private[\s._-]*key|account[\s._-]*(?:private[\s._-]*)?(?:key|secret)|secret|seed(?:[\s._-]*phrase)?|mnemonic|wallet[\s._-]*key)\s*["']?\s*(?::|=|=>)\s*["'`]?$/i;
const highConfidenceSecretPatterns = [
  /-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----/i,
  /\b(?:sk_(?:live|test)|gh[ps]_|xox[baprs]-)[A-Za-z0-9_-]{16,}/i,
  /\bBearer\s+[A-Za-z0-9._-]{20,}/i,
];
const knownAnvilAddresses = [
  "f39fd6e51aad88f6f4ce6ab8827279cfffb92266",
  "70997970c51812dc3a010c7d01b50e0d17dc79c8",
  "3c44cdddb6a900fa2b585dd299e03d12fa4293bc",
  "90f79bf6eb2c4f870365e785982e1f101e93b906",
  "15d34aaf54267db7d7c367839aaf71a00a2c6a65",
  "9965507d1a55bcc2695c58ba16fb37d819b0a4dc",
  "976ea74026e726554db657fa54763abd0c3a0aa9",
  "14dc79964da2c08b23698b3d3cc7ca32193d9955",
  "23618e81e3f5cdf7f54c3d65f7fbc0abf5b21e8f",
  "a0ee7a142d267c1f36714e4a8f75612f20a79720",
].map((value) => value.toLowerCase());
const knownAnvilAddressSet = new Set(knownAnvilAddresses);
const knownAnvilPrivateKeys = new Set([
  "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80",
]);
const knownDependencyAddresses = new Set(
  [
    "0000000000000000000000000000000000000000",
    "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
    "a0b86991c6218b36c1d19d4a2e9eb0ce3606eb48",
    "1c7d4b196cb0c7b01d743fbc6116a902379c7238",
    "0b2c639c533813f4aa9d7837caf62653d097ff85",
    "5fd84259d66cd46123540766be93dfe6d43130d7",
    "3c499c542cef5e3811e1192ce70d8cc03d5c3359",
    "41e94eb019c0762f9bfcf9fb1e58725bfb0e7582",
    "833589fcd6edb6e08f4c7c32d4f71b54bda02913",
    "036cbd53842c5426634e7929541ec2318f3dcf7e",
    "b97ef9ef8734c71904d8002f8b6bc66dd9c48a6e",
    "5425890298aed601595a70ab815c96711a31bc65",
    "75faf114eafb1bdbe2f0316df893fd58ce46aa4d",
    "00000000000c2e074ec69a0dfb2997ba6c7d2e1e",
    "dac17f958d2ee523a2206206994597c13d831ec7",
    "c2132d05d31c914a87c6611c10748aeb04b58e8f",
    "9702230a8ea53601f5cd2dc00fdbc13d4df4a8c7",
    "919c1c267bc06a7039e03fcc2ef738525769109c",
    "48065fbbe25f71c9282ddf5e1cd6d6a887483d5e",
    "55d398326f99059ff775485246999027b3197955",
    "fd086bc7cd5c481dcc9c85ebe478a1c0b69fcbb9",
    "2791bca1f2de4661ed88a30c99a7a9449aa84174",
    "94b008aa00579c1307b0ef2c499ad98a8ce58e58",
    "af88d065e77c8cc2239327c5edb3a432268e5831",
  ].map((value) => value.toLowerCase()),
);

export function filesUnder(directory, predicate) {
  if (!fs.existsSync(directory)) return [];
  const result = [];
  for (const entry of fs.readdirSync(directory, { withFileTypes: true })) {
    const filename = path.join(directory, entry.name);
    if (entry.isDirectory()) result.push(...filesUnder(filename, predicate));
    else if (predicate(filename)) result.push(filename);
  }
  return result;
}

const coreRoutes = ["/", "/graduated", "/create", "/analytics", "/docs", "/profile"];
// Mirrors WALLET_ROUTES in app/providers.tsx.
const walletRoutePattern = /^\/(?:(?:create|profile)(?:[/?]|$)|token\/)/;

/**
 * Version 2 budgets bytes per route class: read-only routes must stay well below wallet routes,
 * which load the Privy/wagmi stack. Navigation time stays a per-viewport profile budget.
 */
export function validatePerformanceBudgetMatrix(matrix = budgetMatrix) {
  const errors = [];
  if (matrix.version !== 2) errors.push("performance budget matrix version must be 2");
  for (const profile of ["small-laptop", "mobile"]) {
    const values = matrix.profiles?.[profile];
    if (!values) {
      errors.push(`performance budget is missing ${profile} profile`);
      continue;
    }
    if (!Number.isSafeInteger(values.navigationMs) || values.navigationMs <= 0)
      errors.push(`${profile} profile is missing a positive navigationMs budget`);
    if (!values.viewport?.width || !values.viewport?.height)
      errors.push(`${profile} profile is missing its viewport`);
  }
  const classes = matrix.routeClasses ?? {};
  for (const name of ["read-only", "wallet"])
    if (!classes[name]) errors.push(`performance budget is missing the ${name} route class`);
  const seen = new Map();
  for (const [name, routeClass] of Object.entries(classes)) {
    for (const key of ["transferredBytes", "initialJavaScriptBytes"])
      if (!Number.isSafeInteger(routeClass[key]) || routeClass[key] <= 0)
        errors.push(`${name} route class is missing a positive ${key} budget`);
    for (const route of routeClass.routes ?? []) {
      if (seen.has(route))
        errors.push(`${route} is budgeted by both ${seen.get(route)} and ${name}`);
      seen.set(route, name);
      if (name === "read-only" && walletRoutePattern.test(route))
        errors.push(`wallet route ${route} cannot use the read-only budget`);
    }
  }
  if (seen.size !== 7) errors.push("performance budget matrix must list all seven core routes");
  for (const route of coreRoutes)
    if (!seen.has(route)) errors.push(`performance budget is missing ${route}`);
  if (
    ![...seen.keys()].some(
      (route) => route.startsWith("/token/") && route.includes("fixture=populated"),
    )
  )
    errors.push("performance budget is missing the populated token fixture route");
  const readOnly = classes["read-only"];
  const wallet = classes.wallet;
  if (readOnly && wallet && !(readOnly.initialJavaScriptBytes < wallet.initialJavaScriptBytes))
    errors.push("read-only initial JavaScript budget must be below the wallet budget");
  return [...new Set(errors)];
}

function reviewedAddressAllowlist() {
  const values = new Set();
  const candidates = [
    path.resolve(process.cwd(), "src/contracts/generated.ts"),
    path.resolve(process.cwd(), "../contracts/deployments/config/robinhood-mainnet.json"),
  ];
  for (const filename of candidates) {
    if (!fs.existsSync(filename)) continue;
    const source = fs.readFileSync(filename, "utf8");
    for (const value of source.matchAll(addressPattern))
      values.add(value[0].slice(2).toLowerCase());
  }
  return values;
}

function isBytecodeOrAbiContext(source, start, end) {
  const before = source.slice(Math.max(0, start - 12), start).toLowerCase();
  const after = source.slice(end, end + 12).toLowerCase();
  // Long contiguous hex strings are bytecode/data blobs, not standalone keys.
  return /[0-9a-f]$/.test(before) || /^[0-9a-f]/.test(after) || before.includes("bytecode");
}

function isKnownGeneratedCryptoContext(source, start, end) {
  const context = source
    .slice(Math.max(0, start - 500), Math.min(source.length, end + 500))
    .toLowerCase();
  return (
    /bigint|bytes32|keccak|secp256k1|curve|modulus|fromseed|asset:|network:|zeroaddress|native|hmac|randombytes|projectivepoint|chainid|tohexstring|uint8array|sig|argumenterror|unsupported type|e6\.from/.test(
      context,
    ) || (context.match(/0x[0-9a-f]{64}/g)?.length ?? 0) >= 2
  );
}

export function scanBundleText(source, filename, allowlist = reviewedAddressAllowlist()) {
  const violations = [];
  for (const pattern of highConfidenceSecretPatterns) {
    if (pattern.test(source)) violations.push(`sensitive value pattern in ${filename}`);
  }
  for (const match of source.matchAll(privateKeyPattern)) {
    const value = match[0].replace(/^0x/i, "").toLowerCase();
    const assignmentContext = source
      .slice(Math.max(0, match.index - 180), match.index)
      .toLowerCase();
    const isSecretAssignment = secretAssignmentPattern.test(assignmentContext);
    // A transaction/block hash has the same shape as a private key. Shape alone
    // is not evidence of leakage; only a secret assignment or an explicit known
    // Anvil key is high-confidence enough to reject it.
    if (!knownAnvilPrivateKeys.has(value) && !isSecretAssignment) continue;
    if (
      !knownAnvilPrivateKeys.has(value) &&
      !isSecretAssignment &&
      (isBytecodeOrAbiContext(source, match.index, match.index + match[0].length) ||
        isKnownGeneratedCryptoContext(source, match.index, match.index + match[0].length))
    )
      continue;
    violations.push(`private-key pattern in ${filename}`);
  }
  for (const match of source.matchAll(addressPattern)) {
    const value = match[0].slice(2).toLowerCase();
    const context = source
      .slice(Math.max(0, match.index - 120), match.index + match[0].length + 120)
      .toLowerCase();
    if (knownAnvilAddressSet.has(value))
      violations.push(`known Anvil address ${match[0]} in ${filename}`);
    else if (/asset:|network:|zeroaddress|native/.test(context)) continue;
    else if (!allowlist.has(value) && !knownDependencyAddresses.has(value))
      violations.push(`unreviewed address ${match[0]} in ${filename}`);
  }
  return [...new Set(violations)];
}

const isMain = process.argv[1] && pathToFileURL(process.argv[1]).href === import.meta.url;

export function evaluateBuildBudget(buildRoot = root) {
  const chunks = filesUnder(path.join(buildRoot, "static", "chunks"), (filename) =>
    filename.endsWith(".js"),
  );
  const total = chunks.reduce((sum, filename) => sum + fs.statSync(filename).size, 0);
  const initial = chunks
    .filter((filename) => /(?:main|app|framework|webpack|polyfills)/i.test(path.basename(filename)))
    .reduce((sum, filename) => sum + fs.statSync(filename).size, 0);
  const manifests = filesUnder(buildRoot, (filename) =>
    /(?:build-manifest|app-build-manifest|routes-manifest)\.json$/.test(filename),
  );
  const largestManifest = manifests.reduce(
    (max, filename) => Math.max(max, fs.statSync(filename).size),
    0,
  );
  const violations = [];
  if (initial > maxInitialJavaScriptBytes)
    violations.push(`initial JavaScript ${initial} > ${maxInitialJavaScriptBytes} bytes`);
  if (total > maxAllJavaScriptBytes)
    violations.push(`all JavaScript ${total} > ${maxAllJavaScriptBytes} bytes`);
  if (largestManifest > maxRouteManifestBytes)
    violations.push(`route manifest ${largestManifest} > ${maxRouteManifestBytes} bytes`);
  return { initial, total, largestManifest, violations };
}

if (isMain) {
  const matrixErrors = validatePerformanceBudgetMatrix();
  if (matrixErrors.length) {
    for (const error of matrixErrors) console.error(`BUDGET_FAIL: ${error}`);
    process.exit(1);
  }
  if (!fs.existsSync(root)) {
    console.error("Performance budget requires a production build at web/.next");
    process.exit(1);
  }

  const { initial, total, largestManifest, violations } = evaluateBuildBudget();
  const allowlist = reviewedAddressAllowlist();
  const bundleFiles = filesUnder(path.join(root, "static"), (filename) =>
    /\.(?:js|html)$/.test(filename),
  );
  for (const filename of bundleFiles) {
    const source = fs.readFileSync(filename, "utf8");
    violations.push(...scanBundleText(source, path.relative(root, filename), allowlist));
  }
  console.log(
    `bundle budget: initial=${initial} bytes, all=${total} bytes, largest-manifest=${largestManifest} bytes`,
  );
  if (violations.length) {
    for (const violation of [...new Set(violations)]) console.error(`BUDGET_FAIL: ${violation}`);
    process.exit(1);
  }
}
