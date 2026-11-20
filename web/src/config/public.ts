import { reviewedDeployments, type ReviewedV4Deployment } from "@/contracts/generated";

// E2E fixture origin; matches the `npm run dev` and Playwright port, kept off 3000 to avoid local collisions.
export const DEFAULT_E2E_WEB_URL = "http://127.0.0.1:3417";

export type ConfiguredDeployment = (typeof reviewedDeployments)[number];

/** The deployment serving each launch engine on the configured chain, or null when absent. */
export type EngineDeployments = { 1: ConfiguredDeployment | null; 2: ConfiguredDeployment | null };

export type PublicConfiguration = {
  status: "ready" | "fail-closed";
  privyAppId: string | null;
  chainId: number | null;
  deploymentId: string | null;
  apiBaseUrl: string | null;
  rpcUrl: string | null;
  /** The site deployment: chain name, explorer and the engine preselected on create. */
  deployment: ConfiguredDeployment | null;
  /**
   * Engine 1 and engine 2 run side by side (docs/specs/2026-11-20-dual-engine-preflight.md).
   * A token's trades and claims use its own engine's deployment.
   */
  engines: EngineDeployments;
};

/** The enabled deployment serving `engineVersion`, or null when that engine is not available. */
export function engineDeployment(
  configuration: Pick<PublicConfiguration, "engines">,
  engineVersion: 1 | 2,
): ConfiguredDeployment | null {
  const deployment = configuration.engines[engineVersion];
  return deployment?.enabled && deployment.engineVersion === engineVersion ? deployment : null;
}

function enginesOf(...deployments: (ConfiguredDeployment | null | undefined)[]): EngineDeployments {
  const engines: EngineDeployments = { 1: null, 2: null };
  for (const deployment of deployments) {
    if (deployment && (deployment.engineVersion === 1 || deployment.engineVersion === 2))
      engines[deployment.engineVersion] = deployment;
  }
  return engines;
}

const nonEmpty = (value: string | undefined) => (value?.trim() ? value.trim() : null);

function publicEndpoint(value: string | null): string | null {
  if (!value) return null;
  try {
    const url = new URL(value);
    const local = url.hostname === "localhost" || url.hostname === "127.0.0.1";
    if (url.protocol !== "https:" && !(local && url.protocol === "http:")) return null;
    if (url.username || url.password || url.search || url.hash) return null;
    return url.toString().replace(/\/$/, "");
  } catch {
    return null;
  }
}

const isAddress = (value: string | null): value is string =>
  value !== null && /^0x[0-9a-fA-F]{40}$/.test(value);

/**
 * The engine-2 Anvil fixture (Plan v2-C Task 2): a local v2 deployment whose every V4 address
 * comes from the gate's environment. Any missing or malformed address leaves the fixture off,
 * so the configuration falls through to the reviewed path and fails closed.
 */
function v2AnvilFixture(
  env: Record<string, string | undefined>,
): Omit<PublicConfiguration, "engines"> | null {
  const read = (name: string) => nonEmpty(env[`NEXT_PUBLIC_V2_ANVIL_${name}`]);
  const factory = read("FACTORY");
  const v4 = {
    poolManager: read("POOL_MANAGER"),
    permit2: read("PERMIT2"),
    universalRouter: read("UNIVERSAL_ROUTER"),
    v4Quoter: read("V4_QUOTER"),
    hook: read("HOOK"),
    locker: read("LOCKER"),
  };
  const fixtureApi = publicEndpoint(nonEmpty(env.NEXT_PUBLIC_TASK6_ANVIL_API_URL));
  const webBase =
    publicEndpoint(nonEmpty(env.NEXT_PUBLIC_TASK6_ANVIL_WEB_URL)) ?? DEFAULT_E2E_WEB_URL;
  const rpcUrl = publicEndpoint(`${webBase}/e2e/rpc`);
  if (
    !isAddress(factory) ||
    !Object.values(v4).every(isAddress) ||
    !publicEndpoint(nonEmpty(env.NEXT_PUBLIC_TASK6_ANVIL_RPC_URL)) ||
    !fixtureApi ||
    !rpcUrl
  )
    return null;
  return {
    status: "ready",
    privyAppId: env.NEXT_PUBLIC_LOCAL_DEV === "1" ? null : "cl_e2e_fixture_1234567890",
    chainId: 31337,
    deploymentId: "v2-anvil",
    apiBaseUrl: fixtureApi,
    rpcUrl,
    deployment: {
      deploymentId: "v2-anvil",
      chainId: 31337,
      name: "Anvil engine v2",
      engineVersion: 2,
      enabled: true,
      factory,
      explorerBase: null,
      marketLinks: null,
      weth: null,
      uniswapV2Factory: null,
      uniswapV2Router02: null,
      v4: v4 as ReviewedV4Deployment,
    },
  };
}

/**
 * The engine-1 Anvil fixture (Task 6): a local v1 deployment whose addresses come from the
 * gate's environment. Any missing or malformed value leaves it off.
 */
function task6AnvilFixture(
  env: Record<string, string | undefined>,
): Omit<PublicConfiguration, "engines"> | null {
  const factory = nonEmpty(env.NEXT_PUBLIC_TASK6_ANVIL_FACTORY);
  const fixtureWeth = nonEmpty(env.NEXT_PUBLIC_TASK6_ANVIL_WETH);
  const fixtureRouter = nonEmpty(env.NEXT_PUBLIC_TASK6_ANVIL_ROUTER);
  const fixtureUniswapFactory = nonEmpty(env.NEXT_PUBLIC_TASK6_ANVIL_UNISWAP_FACTORY);
  const fixtureRpc = publicEndpoint(nonEmpty(env.NEXT_PUBLIC_TASK6_ANVIL_RPC_URL));
  const fixtureApi = publicEndpoint(nonEmpty(env.NEXT_PUBLIC_TASK6_ANVIL_API_URL));
  const webBase =
    publicEndpoint(nonEmpty(env.NEXT_PUBLIC_TASK6_ANVIL_WEB_URL)) ?? DEFAULT_E2E_WEB_URL;
  const rpcUrl = publicEndpoint(`${webBase}/e2e/rpc`);
  if (
    factory &&
    fixtureWeth &&
    fixtureRouter &&
    fixtureUniswapFactory &&
    /^0x[0-9a-fA-F]{40}$/.test(factory) &&
    /^0x[0-9a-fA-F]{40}$/.test(fixtureWeth) &&
    /^0x[0-9a-fA-F]{40}$/.test(fixtureRouter) &&
    /^0x[0-9a-fA-F]{40}$/.test(fixtureUniswapFactory) &&
    fixtureRpc &&
    fixtureApi &&
    rpcUrl
  ) {
    return {
      status: "ready",
      // Local stack previews do not have a real Privy application. Keep the
      // indexed read surface live without initializing an invalid auth client.
      privyAppId: env.NEXT_PUBLIC_LOCAL_DEV === "1" ? null : "cl_e2e_fixture_1234567890",
      chainId: 31337,
      deploymentId: "task6-anvil",
      apiBaseUrl: fixtureApi,
      rpcUrl,
      deployment: {
        deploymentId: "task6-anvil",
        chainId: 31337,
        name: "Anvil Task 6",
        enabled: true,
        factory,
        explorerBase: null,
        marketLinks: null,
        weth: fixtureWeth,
        uniswapV2Factory: fixtureUniswapFactory,
        uniswapV2Router02: fixtureRouter,
        engineVersion: 1,
        v4: null,
      } as (typeof reviewedDeployments)[number],
    };
  }
  return null;
}

/**
 * The local Anvil fixtures (Task 6 engine 1, Plan v2-C Task 2 engine 2) sign through an
 * injected wallet with no identity provider; every reviewed deployment goes through Privy.
 */
export function isAnvilWalletFixture(configuration: Pick<PublicConfiguration, "deploymentId">) {
  return configuration.deploymentId === "task6-anvil" || configuration.deploymentId === "v2-anvil";
}

export function publicConfiguration(
  env: Record<string, string | undefined> = {
    NEXT_PUBLIC_E2E_FIXTURE: process.env.NEXT_PUBLIC_E2E_FIXTURE,
    NEXT_PUBLIC_LOCAL_DEV: process.env.NEXT_PUBLIC_LOCAL_DEV,
    NEXT_PUBLIC_TASK6_ANVIL_FACTORY: process.env.NEXT_PUBLIC_TASK6_ANVIL_FACTORY,
    NEXT_PUBLIC_TASK6_ANVIL_WETH: process.env.NEXT_PUBLIC_TASK6_ANVIL_WETH,
    NEXT_PUBLIC_TASK6_ANVIL_ROUTER: process.env.NEXT_PUBLIC_TASK6_ANVIL_ROUTER,
    NEXT_PUBLIC_TASK6_ANVIL_UNISWAP_FACTORY: process.env.NEXT_PUBLIC_TASK6_ANVIL_UNISWAP_FACTORY,
    NEXT_PUBLIC_TASK6_ANVIL_RPC_URL: process.env.NEXT_PUBLIC_TASK6_ANVIL_RPC_URL,
    NEXT_PUBLIC_TASK6_ANVIL_API_URL: process.env.NEXT_PUBLIC_TASK6_ANVIL_API_URL,
    NEXT_PUBLIC_TASK6_ANVIL_WEB_URL: process.env.NEXT_PUBLIC_TASK6_ANVIL_WEB_URL,
    NEXT_PUBLIC_V2_ANVIL_FACTORY: process.env.NEXT_PUBLIC_V2_ANVIL_FACTORY,
    NEXT_PUBLIC_V2_ANVIL_POOL_MANAGER: process.env.NEXT_PUBLIC_V2_ANVIL_POOL_MANAGER,
    NEXT_PUBLIC_V2_ANVIL_PERMIT2: process.env.NEXT_PUBLIC_V2_ANVIL_PERMIT2,
    NEXT_PUBLIC_V2_ANVIL_UNIVERSAL_ROUTER: process.env.NEXT_PUBLIC_V2_ANVIL_UNIVERSAL_ROUTER,
    NEXT_PUBLIC_V2_ANVIL_V4_QUOTER: process.env.NEXT_PUBLIC_V2_ANVIL_V4_QUOTER,
    NEXT_PUBLIC_V2_ANVIL_HOOK: process.env.NEXT_PUBLIC_V2_ANVIL_HOOK,
    NEXT_PUBLIC_V2_ANVIL_LOCKER: process.env.NEXT_PUBLIC_V2_ANVIL_LOCKER,
    NEXT_PUBLIC_PRIVY_APP_ID: process.env.NEXT_PUBLIC_PRIVY_APP_ID,
    NEXT_PUBLIC_DEPLOYMENT_ID: process.env.NEXT_PUBLIC_DEPLOYMENT_ID,
    NEXT_PUBLIC_ENGINE_DEPLOYMENT_IDS: process.env.NEXT_PUBLIC_ENGINE_DEPLOYMENT_IDS,
    NEXT_PUBLIC_CHAIN_ID: process.env.NEXT_PUBLIC_CHAIN_ID,
    NEXT_PUBLIC_API_BASE_URL: process.env.NEXT_PUBLIC_API_BASE_URL,
    NEXT_PUBLIC_RPC_URL: process.env.NEXT_PUBLIC_RPC_URL,
  },
): PublicConfiguration {
  if (env.NEXT_PUBLIC_E2E_FIXTURE === "1") {
    // A local chain may run either engine or both; engine 1 is the site deployment when present.
    const v1 = task6AnvilFixture(env);
    const v2 = v2AnvilFixture(env);
    const site = v1 ?? v2;
    if (site) return { ...site, engines: enginesOf(v1?.deployment, v2?.deployment) };
  }
  const privyAppId = nonEmpty(env.NEXT_PUBLIC_PRIVY_APP_ID);
  const deploymentId = nonEmpty(env.NEXT_PUBLIC_DEPLOYMENT_ID);
  const chainValue = nonEmpty(env.NEXT_PUBLIC_CHAIN_ID);
  const parsedChainId = chainValue && /^\d+$/.test(chainValue) ? Number(chainValue) : NaN;
  const chainId = Number.isSafeInteger(parsedChainId) && parsedChainId > 0 ? parsedChainId : null;
  const apiBaseUrl = publicEndpoint(nonEmpty(env.NEXT_PUBLIC_API_BASE_URL));
  const rpcUrl = publicEndpoint(nonEmpty(env.NEXT_PUBLIC_RPC_URL));
  const deployment = reviewedDeployments.find(
    (candidate) => candidate.deploymentId === deploymentId && candidate.chainId === chainId,
  );
  const engines = reviewedEngines(env.NEXT_PUBLIC_ENGINE_DEPLOYMENT_IDS, deployment, chainId);
  const status =
    deployment?.enabled && engines && privyAppId && apiBaseUrl && rpcUrl ? "ready" : "fail-closed";
  return {
    status,
    privyAppId,
    chainId,
    deploymentId,
    apiBaseUrl,
    rpcUrl,
    deployment: deployment ?? null,
    engines: engines ?? enginesOf(deployment),
  };
}

/**
 * The reviewed deployment of each engine, mirroring the backend's ENGINE_DEPLOYMENT_IDS: a
 * comma-separated list that includes the site deployment, every id reviewed on the same chain
 * and at most one per engine. Unset means the site deployment alone; an invalid list is null, so
 * the configuration fails closed.
 */
function reviewedEngines(
  raw: string | undefined,
  site: ConfiguredDeployment | undefined,
  chainId: number | null,
): EngineDeployments | null {
  if (!site) return null;
  const list = nonEmpty(raw);
  if (!list) return enginesOf(site);
  const ids = list.split(",").map((id) => id.trim());
  if (new Set(ids).size !== ids.length || !ids.includes(site.deploymentId)) return null;
  const deployments = ids.map((id) =>
    reviewedDeployments.find(
      (candidate) => candidate.deploymentId === id && candidate.chainId === chainId,
    ),
  );
  if (deployments.some((deployment) => !deployment)) return null;
  const versions = deployments.map((deployment) => deployment!.engineVersion);
  if (new Set(versions).size !== versions.length) return null;
  return enginesOf(...deployments);
}
