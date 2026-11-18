import fs from "node:fs";
import path from "node:path";
import process from "node:process";
import { pathToFileURL } from "node:url";

const production = process.argv.includes("--production");
const anvil = process.argv.includes("--anvil");
const required = [
  "NEXT_PUBLIC_PRIVY_APP_ID",
  "NEXT_PUBLIC_DEPLOYMENT_ID",
  "NEXT_PUBLIC_CHAIN_ID",
  "NEXT_PUBLIC_API_BASE_URL",
  "NEXT_PUBLIC_RPC_URL",
];
// A fixture word must start a token ("testnet", "e2e-rpc"), so random IDs that merely contain
// the letters ("cmabe2e9", "latest") are not rejected. Mirrors src/config/release.ts.
const fixture = /(?:^|[^a-z0-9])(?:task6|e2e|anvil|localhost|test|mock|fixture)|127\.0\.0\.1/i;
const v4AddressKeys = ["poolManager", "permit2", "universalRouter", "v4Quoter", "hook", "locker"];

// reviewedAddressesComplete checks the addresses the web app needs for the deployment's engine:
// Uniswap v2 for engine 1, the v4 set for engine 2 (whose v2 fields are null by design).
function reviewedAddressesComplete(deployment) {
  if (!deployment.factory) return false;
  if (deployment.engineVersion === 2)
    return v4AddressKeys.every((key) => Boolean(deployment.v4?.[key]));
  return [deployment.weth, deployment.uniswapV2Factory, deployment.uniswapV2Router02].every(
    Boolean,
  );
}

export function readReviewedDeployments(
  generatedPath = path.resolve(process.cwd(), "src/contracts/generated.ts"),
) {
  if (!fs.existsSync(generatedPath))
    return { deployments: [], error: "reviewed deployment manifest is missing" };
  const generated = fs.readFileSync(generatedPath, "utf8");
  const deploymentMatch = generated.match(
    /export const reviewedDeployments:[^=]+=\s*(\[[\s\S]*?\]);/,
  );
  if (!deploymentMatch)
    return { deployments: [], error: "reviewed deployment manifest is not generated" };
  try {
    return { deployments: JSON.parse(deploymentMatch[1]), error: undefined };
  } catch {
    return { deployments: [], error: "reviewed deployment manifest is not valid generated JSON" };
  }
}

/**
 * Legal documents (src/legal/documents.json) must be approved by counsel before a production
 * release (docs/specs/2026-11-18-legal-pages-preflight.md).
 */
export function readLegalDocuments(
  documentsPath = path.resolve(process.cwd(), "src/legal/documents.json"),
) {
  try {
    return JSON.parse(fs.readFileSync(documentsPath, "utf8"));
  } catch {
    return null;
  }
}

export function legalDocumentErrors(documents) {
  if (!documents || typeof documents !== "object") return ["legal documents status is unreadable"];
  const errors = [];
  for (const id of ["terms", "privacy"]) {
    const document = documents[id];
    if (document?.status !== "approved")
      errors.push(
        `legal document ${id} is not approved by counsel (status ${document?.status ?? "missing"})`,
      );
    else if (!/^\d{4}-\d{2}-\d{2}$/.test(document.effectiveDate ?? ""))
      errors.push(`legal document ${id} needs an effective date (YYYY-MM-DD)`);
  }
  return errors;
}

function validateUrl(value, key, { allowHttp = false, originOnly = false } = {}) {
  if (!value?.trim()) return `${key} is required`;
  try {
    const url = new URL(value);
    if (url.username || url.password || url.search || url.hash)
      return `${key} contains credentials or query material`;
    if (url.protocol !== "https:" && !(allowHttp && url.protocol === "http:"))
      return `${key} must use https outside Anvil`;
    if (originOnly && url.pathname !== "/") return `${key} must be an origin without a path`;
  } catch {
    return `${key} is not a valid URL`;
  }
  return undefined;
}

export function validateReleaseEnvironment(
  env = process.env,
  mode = production ? "production" : anvil ? "anvil" : "unknown",
  deployments = readReviewedDeployments().deployments,
  legalDocuments = readLegalDocuments(),
) {
  const errors = [];
  if (mode !== "production" && mode !== "anvil")
    errors.push("release mode must be production or anvil");
  if (mode === "production")
    for (const key of required) if (!env[key]?.trim()) errors.push(`${key} is required`);
  if (mode === "production") {
    if (env.NEXT_PUBLIC_E2E_FIXTURE?.trim() === "1") errors.push("E2E fixture is not deployable");
    for (const key of required)
      if (fixture.test(env[key] ?? "")) errors.push(`${key} contains a fixture or test value`);
    const originError = validateUrl(env.NEXT_PUBLIC_WEB_ORIGIN, "NEXT_PUBLIC_WEB_ORIGIN", {
      originOnly: true,
    });
    if (originError) errors.push(originError);
    for (const key of ["NEXT_PUBLIC_API_BASE_URL", "NEXT_PUBLIC_RPC_URL"]) {
      const urlError = validateUrl(env[key], key);
      if (urlError) errors.push(urlError);
    }
    const manifest = readReviewedDeployments();
    if (manifest.error) errors.push(manifest.error);
    const candidates = deployments.length ? deployments : manifest.deployments;
    const deployment = candidates.find(
      (candidate) =>
        candidate.deploymentId === env.NEXT_PUBLIC_DEPLOYMENT_ID &&
        String(candidate.chainId) === env.NEXT_PUBLIC_CHAIN_ID,
    );
    errors.push(...legalDocumentErrors(legalDocuments));
    if (!deployment) errors.push("deployment ID and chain ID are not in the reviewed manifest");
    else if (!deployment.enabled) errors.push("deployment is disabled in the reviewed manifest");
    else if (!reviewedAddressesComplete(deployment))
      errors.push("enabled deployment is missing reviewed contract addresses");
  } else if (mode === "anvil" && env.NEXT_PUBLIC_E2E_FIXTURE?.trim() !== "1") {
    errors.push("Anvil gate requires NEXT_PUBLIC_E2E_FIXTURE=1");
  }
  if (mode === "anvil") {
    for (const key of ["NEXT_PUBLIC_API_BASE_URL", "NEXT_PUBLIC_RPC_URL"])
      if (env[key]) {
        const urlError = validateUrl(env[key], key, { allowHttp: true });
        if (urlError) errors.push(urlError);
      }
  }
  return [...new Set(errors)];
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  const mode = production ? "production" : anvil ? "anvil" : "unknown";
  const errors = validateReleaseEnvironment(process.env, mode);
  if (errors.length) {
    console.error("Release validation failed:");
    for (const error of errors) console.error(`- ${error}`);
    process.exit(1);
  }
  console.log(`Release configuration validated (${mode}).`);
}
