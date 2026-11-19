import { describe, expect, it } from "vitest";
import { legalDocumentErrors, validateReleaseEnvironment } from "./validate-release.mjs";

const reviewed = [
  {
    deploymentId: "production",
    chainId: 46630,
    enabled: true,
    factory: "0x0000000000000000000000000000000000000001",
    weth: "0x0000000000000000000000000000000000000002",
    uniswapV2Factory: "0x0000000000000000000000000000000000000003",
    uniswapV2Router02: "0x0000000000000000000000000000000000000004",
  },
];
const approved = {
  terms: { status: "approved", effectiveDate: "2027-01-01" },
  privacy: { status: "approved", effectiveDate: "2027-01-01" },
};
const valid = {
  NEXT_PUBLIC_PRIVY_APP_ID: "cl_live_1234567890",
  NEXT_PUBLIC_DEPLOYMENT_ID: "production",
  NEXT_PUBLIC_CHAIN_ID: "46630",
  NEXT_PUBLIC_API_BASE_URL: "https://api.example",
  NEXT_PUBLIC_RPC_URL: "https://rpc.example",
  NEXT_PUBLIC_WEB_ORIGIN: "https://launchpad.example",
};

describe("release target validation", () => {
  it("fails closed for an unknown target and missing values", () => {
    expect(validateReleaseEnvironment({}, "staging")).toContain(
      "release mode must be production or anvil",
    );
    expect(validateReleaseEnvironment({}, "production")).toContain(
      "NEXT_PUBLIC_WEB_ORIGIN is required",
    );
  });

  it("accepts only a complete reviewed production configuration", () => {
    expect(validateReleaseEnvironment(valid, "production", reviewed, approved)).toEqual([]);
    expect(
      validateReleaseEnvironment(
        { ...valid, NEXT_PUBLIC_DEPLOYMENT_ID: "unknown" },
        "production",
        reviewed,
        approved,
      ),
    ).toContain("deployment ID and chain ID are not in the reviewed manifest");
    expect(
      validateReleaseEnvironment(
        { ...valid, NEXT_PUBLIC_E2E_FIXTURE: "1" },
        "production",
        reviewed,
        approved,
      ),
    ).toContain("E2E fixture is not deployable");
  });

  it("validates engine 2 deployments against their v4 addresses", () => {
    const v4 = {
      poolManager: "0x0000000000000000000000000000000000000011",
      permit2: "0x0000000000000000000000000000000000000012",
      universalRouter: "0x0000000000000000000000000000000000000013",
      v4Quoter: "0x0000000000000000000000000000000000000014",
      hook: "0x0000000000000000000000000000000000000015",
      locker: "0x0000000000000000000000000000000000000016",
    };
    const engine2 = {
      ...reviewed[0],
      engineVersion: 2,
      weth: null,
      uniswapV2Factory: null,
      uniswapV2Router02: null,
      v4,
    };
    expect(validateReleaseEnvironment(valid, "production", [engine2], approved)).toEqual([]);
    expect(
      validateReleaseEnvironment(
        valid,
        "production",
        [{ ...engine2, v4: { ...v4, locker: null } }],
        approved,
      ),
    ).toContain("enabled deployment is missing reviewed contract addresses");
    expect(
      validateReleaseEnvironment(valid, "production", [{ ...engine2, v4: null }], approved),
    ).toContain("enabled deployment is missing reviewed contract addresses");
  });

  it("rejects fixture words only at a token start", () => {
    for (const value of ["cmabe2e9contest", "cl_latest_attest"])
      expect(
        validateReleaseEnvironment(
          { ...valid, NEXT_PUBLIC_PRIVY_APP_ID: value },
          "production",
          reviewed,
          approved,
        ),
      ).toEqual([]);
    for (const value of [
      "https://rpc.testnet.example",
      "https://e2e-rpc.example",
      "http://127.0.0.1",
    ])
      expect(
        validateReleaseEnvironment(
          { ...valid, NEXT_PUBLIC_RPC_URL: value },
          "production",
          reviewed,
          approved,
        ),
      ).toContain("NEXT_PUBLIC_RPC_URL contains a fixture or test value");
  });

  it("requires explicit fixture mode for deterministic Anvil", () => {
    expect(validateReleaseEnvironment({ ...valid }, "anvil", reviewed)).toContain(
      "Anvil gate requires NEXT_PUBLIC_E2E_FIXTURE=1",
    );
    expect(
      validateReleaseEnvironment({ ...valid, NEXT_PUBLIC_E2E_FIXTURE: "1" }, "anvil", reviewed),
    ).toEqual([]);
  });

  it("refuses a production release while a legal document is a draft", () => {
    const draft = { ...approved, terms: { status: "draft", effectiveDate: null } };
    expect(validateReleaseEnvironment(valid, "production", reviewed, draft)).toEqual([
      "legal document terms is not approved by counsel (status draft)",
    ]);
    expect(legalDocumentErrors(null)).toEqual(["legal documents status is unreadable"]);
    expect(legalDocumentErrors({ terms: approved.terms })).toEqual([
      "legal document privacy is not approved by counsel (status missing)",
    ]);
    expect(
      legalDocumentErrors({ ...approved, privacy: { status: "approved", effectiveDate: "soon" } }),
    ).toEqual(["legal document privacy needs an effective date (YYYY-MM-DD)"]);
    expect(validateReleaseEnvironment(valid, "anvil", reviewed, draft)).not.toContain(
      "legal document terms is not approved by counsel (status draft)",
    );
  });
});
