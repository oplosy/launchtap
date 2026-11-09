import fs from "node:fs";
import { pathToFileURL } from "node:url";
import { validateReleaseEnvironment } from "./validate-release.mjs";

export const publicKeys = [
  "NEXT_PUBLIC_PRIVY_APP_ID",
  "NEXT_PUBLIC_DEPLOYMENT_ID",
  "NEXT_PUBLIC_CHAIN_ID",
  "NEXT_PUBLIC_API_BASE_URL",
  "NEXT_PUBLIC_RPC_URL",
  "NEXT_PUBLIC_WEB_ORIGIN",
];

/**
 * The server-side API base must be a plain http(s) URL the container can reach. When it is unset
 * the web container falls back to NEXT_PUBLIC_API_BASE_URL, which must then not be a loopback
 * address (inside the container that is the container itself, not the API).
 */
export function apiInternalErrors(env) {
  const value = env.API_INTERNAL_BASE_URL?.trim();
  if (!value) {
    const publicBase = env.NEXT_PUBLIC_API_BASE_URL?.trim();
    if (!publicBase) return [];
    try {
      const host = new URL(publicBase).hostname;
      if (["localhost", "127.0.0.1", "[::1]", "::1"].includes(host))
        return [
          "API_INTERNAL_BASE_URL is required when NEXT_PUBLIC_API_BASE_URL is not reachable from the container",
        ];
    } catch {
      return [];
    }
    return [];
  }
  try {
    const url = new URL(value);
    if (url.protocol !== "http:" && url.protocol !== "https:")
      return ["API_INTERNAL_BASE_URL must use http or https"];
    if (url.username || url.password || url.search || url.hash)
      return ["API_INTERNAL_BASE_URL contains credentials or query material"];
  } catch {
    return ["API_INTERNAL_BASE_URL is not a valid URL"];
  }
  return [];
}

export function configurationDrift(built, runtime) {
  return publicKeys
    .filter((key) => (built[key] ?? "") !== (runtime[key] ?? ""))
    .map((key) => `${key} differs from the built image; rebuild before promotion`);
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  if (process.argv.includes("--record")) {
    fs.writeFileSync(
      "public-config.json",
      JSON.stringify(Object.fromEntries(publicKeys.map((key) => [key, process.env[key] ?? ""]))),
    );
  } else {
    const built = JSON.parse(fs.readFileSync("public-config.json", "utf8"));
    const errors = [
      ...configurationDrift(built, process.env),
      ...apiInternalErrors(process.env),
      ...validateReleaseEnvironment(built, "production"),
    ];
    if (errors.length) {
      for (const error of errors) console.error(error);
      process.exit(1);
    }
    console.log("Production container configuration matches its reviewed build.");
  }
}
