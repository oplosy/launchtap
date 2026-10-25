import { reviewedDeployments } from "@/contracts/generated";

// E2E fixture origin; matches the `npm run dev` and Playwright port, kept off 3000 to avoid local collisions.
export const DEFAULT_E2E_WEB_URL = "http://127.0.0.1:3417";

export type PublicConfiguration = {
  status: "ready" | "fail-closed";
  privyAppId: string | null;
  chainId: number | null;
  deploymentId: string | null;
  apiBaseUrl: string | null;
  rpcUrl: string | null;
  deployment: (typeof reviewedDeployments)[number] | null;
};

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
    NEXT_PUBLIC_PRIVY_APP_ID: process.env.NEXT_PUBLIC_PRIVY_APP_ID,
    NEXT_PUBLIC_DEPLOYMENT_ID: process.env.NEXT_PUBLIC_DEPLOYMENT_ID,
    NEXT_PUBLIC_CHAIN_ID: process.env.NEXT_PUBLIC_CHAIN_ID,
    NEXT_PUBLIC_API_BASE_URL: process.env.NEXT_PUBLIC_API_BASE_URL,
    NEXT_PUBLIC_RPC_URL: process.env.NEXT_PUBLIC_RPC_URL,
  },
): PublicConfiguration {
  if (env.NEXT_PUBLIC_E2E_FIXTURE === "1") {
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
          weth: fixtureWeth,
          uniswapV2Factory: fixtureUniswapFactory,
          uniswapV2Router02: fixtureRouter,
        } as (typeof reviewedDeployments)[number],
      };
    }
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
  const status =
    deployment?.enabled && privyAppId && apiBaseUrl && rpcUrl ? "ready" : "fail-closed";
  return {
    status,
    privyAppId,
    chainId,
    deploymentId,
    apiBaseUrl,
    rpcUrl,
    deployment: deployment ?? null,
  };
}
