import { describe, expect, it } from "vitest";
import { publicConfiguration } from "./public";

describe("publicConfiguration", () => {
  it("fails closed for missing values", () => {
    expect(publicConfiguration({})).toMatchObject({ status: "fail-closed", chainId: null });
  });

  it("fails closed for an unknown or wrong-chain deployment", () => {
    const env = {
      NEXT_PUBLIC_DEPLOYMENT_ID: "robinhood-mainnet",
      NEXT_PUBLIC_CHAIN_ID: "46630",
      NEXT_PUBLIC_API_BASE_URL: "https://api.example",
      NEXT_PUBLIC_RPC_URL: "https://rpc.example",
    };
    expect(publicConfiguration(env).status).toBe("fail-closed");
  });

  it("resolves the reviewed Robinhood testnet deployment", () => {
    const configuration = publicConfiguration({
      NEXT_PUBLIC_PRIVY_APP_ID: "cl_test_1234567890",
      NEXT_PUBLIC_DEPLOYMENT_ID: "robinhood-testnet-v1",
      NEXT_PUBLIC_CHAIN_ID: "46630",
      NEXT_PUBLIC_API_BASE_URL: "https://api.testnet.example",
      NEXT_PUBLIC_RPC_URL: "https://rpc.testnet.example",
    });

    expect(configuration.status).toBe("ready");
    expect(configuration.deployment?.factory).toBe("0xedddb61a53226ffdc6ecf7c042b803e769168d55");
  });

  it("uses the real backend URL for the Anvil fixture instead of a receipt fixture", () => {
    const configuration = publicConfiguration({
      NEXT_PUBLIC_E2E_FIXTURE: "1",
      NEXT_PUBLIC_TASK6_ANVIL_FACTORY: "0x0000000000000000000000000000000000000001",
      NEXT_PUBLIC_TASK6_ANVIL_WETH: "0x0000000000000000000000000000000000000002",
      NEXT_PUBLIC_TASK6_ANVIL_ROUTER: "0x0000000000000000000000000000000000000003",
      NEXT_PUBLIC_TASK6_ANVIL_UNISWAP_FACTORY: "0x0000000000000000000000000000000000000004",
      NEXT_PUBLIC_TASK6_ANVIL_RPC_URL: "http://127.0.0.1:8545",
      NEXT_PUBLIC_TASK6_ANVIL_API_URL: "http://127.0.0.1:18080",
      NEXT_PUBLIC_TASK6_ANVIL_WEB_URL: "http://127.0.0.1:3000",
    });
    expect(configuration.status).toBe("ready");
    expect(configuration.apiBaseUrl).toBe("http://127.0.0.1:18080");
  });

  it("does not initialize a fake identity client for the local Anvil preview", () => {
    const configuration = publicConfiguration({
      NEXT_PUBLIC_E2E_FIXTURE: "1",
      NEXT_PUBLIC_LOCAL_DEV: "1",
      NEXT_PUBLIC_TASK6_ANVIL_FACTORY: "0x0000000000000000000000000000000000000001",
      NEXT_PUBLIC_TASK6_ANVIL_WETH: "0x0000000000000000000000000000000000000002",
      NEXT_PUBLIC_TASK6_ANVIL_ROUTER: "0x0000000000000000000000000000000000000003",
      NEXT_PUBLIC_TASK6_ANVIL_UNISWAP_FACTORY: "0x0000000000000000000000000000000000000004",
      NEXT_PUBLIC_TASK6_ANVIL_RPC_URL: "http://127.0.0.1:8545",
      NEXT_PUBLIC_TASK6_ANVIL_API_URL: "http://127.0.0.1:18080",
      NEXT_PUBLIC_TASK6_ANVIL_WEB_URL: "http://127.0.0.1:3000",
    });

    expect(configuration.status).toBe("ready");
    expect(configuration.privyAppId).toBeNull();
  });

  const v2Fixture = {
    NEXT_PUBLIC_E2E_FIXTURE: "1",
    NEXT_PUBLIC_V2_ANVIL_FACTORY: "0x0000000000000000000000000000000000000011",
    NEXT_PUBLIC_V2_ANVIL_POOL_MANAGER: "0x0000000000000000000000000000000000000012",
    NEXT_PUBLIC_V2_ANVIL_PERMIT2: "0x000000000022D473030F116dDEE9F6B43aC78BA3",
    NEXT_PUBLIC_V2_ANVIL_UNIVERSAL_ROUTER: "0x0000000000000000000000000000000000000013",
    NEXT_PUBLIC_V2_ANVIL_V4_QUOTER: "0x0000000000000000000000000000000000000014",
    NEXT_PUBLIC_V2_ANVIL_HOOK: "0x0000000000000000000000000000000000002000",
    NEXT_PUBLIC_V2_ANVIL_LOCKER: "0x0000000000000000000000000000000000000015",
    NEXT_PUBLIC_TASK6_ANVIL_RPC_URL: "http://127.0.0.1:8545",
    NEXT_PUBLIC_TASK6_ANVIL_API_URL: "http://127.0.0.1:18080",
    NEXT_PUBLIC_TASK6_ANVIL_WEB_URL: "http://127.0.0.1:3000",
  };

  it("builds an engine-2 Anvil fixture deployment with every reviewed V4 address", () => {
    const configuration = publicConfiguration(v2Fixture);
    expect(configuration.status).toBe("ready");
    expect(configuration.deploymentId).toBe("v2-anvil");
    expect(configuration.deployment?.engineVersion).toBe(2);
    expect(configuration.deployment?.uniswapV2Router02).toBeNull();
    expect(configuration.deployment?.v4).toEqual({
      poolManager: v2Fixture.NEXT_PUBLIC_V2_ANVIL_POOL_MANAGER,
      permit2: v2Fixture.NEXT_PUBLIC_V2_ANVIL_PERMIT2,
      universalRouter: v2Fixture.NEXT_PUBLIC_V2_ANVIL_UNIVERSAL_ROUTER,
      v4Quoter: v2Fixture.NEXT_PUBLIC_V2_ANVIL_V4_QUOTER,
      hook: v2Fixture.NEXT_PUBLIC_V2_ANVIL_HOOK,
      locker: v2Fixture.NEXT_PUBLIC_V2_ANVIL_LOCKER,
    });
  });

  it("fails closed when the engine-2 Anvil fixture misses any V4 address", () => {
    for (const key of Object.keys(v2Fixture).filter((name) => name.startsWith("NEXT_PUBLIC_V2_"))) {
      expect(publicConfiguration({ ...v2Fixture, [key]: "" }).status).toBe("fail-closed");
    }
  });

  it("rejects credentials or query material in public endpoints", () => {
    expect(
      publicConfiguration({
        NEXT_PUBLIC_API_BASE_URL: "https://user:pass@example.test",
        NEXT_PUBLIC_RPC_URL: "https://rpc.example?token=secret",
      }).apiBaseUrl,
    ).toBeNull();
    expect(
      publicConfiguration({ NEXT_PUBLIC_API_BASE_URL: "https://api.example?token=secret" })
        .apiBaseUrl,
    ).toBeNull();
  });
});
