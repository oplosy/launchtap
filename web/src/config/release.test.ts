import { describe, expect, it } from "vitest";
import { validateReleaseEnvironment } from "./release";

const valid = {
  NEXT_PUBLIC_PRIVY_APP_ID: "cl_live_1234567890",
  NEXT_PUBLIC_DEPLOYMENT_ID: "robinhood-mainnet",
  NEXT_PUBLIC_CHAIN_ID: "46630",
  NEXT_PUBLIC_API_BASE_URL: "https://api.launchpad.example",
  NEXT_PUBLIC_RPC_URL: "https://rpc.launchpad.example",
  NEXT_PUBLIC_WEB_ORIGIN: "https://launchpad.example",
};

describe("validateReleaseEnvironment", () => {
  it("fails closed for missing public configuration", () => {
    expect(validateReleaseEnvironment({}).ok).toBe(false);
  });

  it("rejects fixture values from production", () => {
    expect(
      validateReleaseEnvironment({ ...valid, NEXT_PUBLIC_DEPLOYMENT_ID: "task6-anvil" }).errors,
    ).toContain("NEXT_PUBLIC_DEPLOYMENT_ID contains a fixture or test value");
  });

  it("rejects fixture words only at a token start", () => {
    expect(
      validateReleaseEnvironment({ ...valid, NEXT_PUBLIC_PRIVY_APP_ID: "cmabe2e9contest" }).errors,
    ).not.toContain("NEXT_PUBLIC_PRIVY_APP_ID contains a fixture or test value");
    expect(
      validateReleaseEnvironment({ ...valid, NEXT_PUBLIC_RPC_URL: "https://rpc.testnet.example" })
        .errors,
    ).toContain("NEXT_PUBLIC_RPC_URL contains a fixture or test value");
  });

  it("rejects an unreviewed or disabled deployment", () => {
    expect(validateReleaseEnvironment(valid).errors).toEqual(
      expect.arrayContaining([
        "reviewed public deployment configuration is incomplete",
        "deployment is not enabled in the reviewed manifest",
      ]),
    );
  });

  it("requires the explicit fixture mode for Anvil evidence", () => {
    expect(validateReleaseEnvironment(valid, "anvil").errors).toContain(
      "Anvil validation requires NEXT_PUBLIC_E2E_FIXTURE=1",
    );
  });
});
