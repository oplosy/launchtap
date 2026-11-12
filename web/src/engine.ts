import type { ReviewedV4Deployment } from "@/contracts/generated";

export type EngineVersion = 1 | 2;

/** The engine a token was launched on, from the API. Any other value is refused. */
export function engineOf(token: { engine_version: number }): EngineVersion {
  if (token.engine_version === 1 || token.engine_version === 2) return token.engine_version;
  throw new Error(`unsupported engine ${token.engine_version}`);
}

export type Address = `0x${string}`;

export type V4Addresses = {
  poolManager: Address;
  permit2: Address;
  universalRouter: Address;
  v4Quoter: Address;
  hook: Address;
  locker: Address;
};

const isAddress = (value: string | null | undefined): value is Address =>
  typeof value === "string" && /^0x[0-9a-fA-F]{40}$/.test(value);

/**
 * The reviewed V4 addresses the browser may sign against, or null when the deployment is not
 * an enabled engine-2 deployment on the expected chain or any address is missing or malformed
 * (Plan v2-C D5, mirrors reviewedRouterAddress for v1).
 */
export function reviewedV4Addresses(
  deployment: {
    enabled: boolean;
    chainId: number;
    engineVersion: EngineVersion;
    v4: ReviewedV4Deployment | null;
  },
  expectedChainId: number | null,
): V4Addresses | null {
  if (!deployment.enabled || deployment.engineVersion !== 2 || !deployment.v4) return null;
  if (deployment.chainId !== expectedChainId) return null;
  const { poolManager, permit2, universalRouter, v4Quoter, hook, locker } = deployment.v4;
  if (
    !isAddress(poolManager) ||
    !isAddress(permit2) ||
    !isAddress(universalRouter) ||
    !isAddress(v4Quoter) ||
    !isAddress(hook) ||
    !isAddress(locker)
  )
    return null;
  return { poolManager, permit2, universalRouter, v4Quoter, hook, locker };
}
