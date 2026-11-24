// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { useState } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { TransactionState } from "@/transactions";

const ACCOUNT = "0x00000000000000000000000000000000000000a1";
const v4 = {
  poolManager: "0x0000000000000000000000000000000000000001",
  permit2: "0x0000000000000000000000000000000000000002",
  universalRouter: "0x0000000000000000000000000000000000000003",
  v4Quoter: "0x0000000000000000000000000000000000000004",
  hook: "0x0000000000000000000000000000000000000005",
  locker: "0x0000000000000000000000000000000000000006",
};

const setup = { chainId: 4663 };
const simulateContract = vi.fn();

vi.mock("wagmi", () => ({
  useAccount: () => ({ address: ACCOUNT }),
  usePublicClient: () => ({ simulateContract }),
  useWalletClient: () => ({ data: { getAddresses: vi.fn() } }),
}));
vi.mock("@/wallet/readiness", () => ({
  useWalletReadiness: () => ({
    address: ACCOUNT,
    chainId: setup.chainId,
    selectedAccountVerified: true,
    switchNetwork: vi.fn(),
  }),
}));
vi.mock("@/wallet/connection-context", () => ({ WalletConnectButton: () => null }));
vi.mock("@/wallet/explorer", () => ({ addressExplorerUrl: () => null }));
vi.mock("@/market/usd-line", () => ({ TradeUsdHint: () => null }));
// The side tabs animate an indicator with ResizeObserver, which jsdom lacks.
vi.mock("@/components/sliding-indicator", () => ({ SlidingIndicator: () => null }));
vi.mock("@/config/public", () => {
  const deployment = {
    deploymentId: "robinhood-mainnet-v2",
    chainId: 4663,
    name: "Robinhood Chain",
    engineVersion: 2,
    enabled: true,
    v4,
  };
  const configuration = () => ({
    status: "ready",
    chainId: setup.chainId,
    apiBaseUrl: null,
    deployment,
    engines: { 1: null, 2: { ...deployment, chainId: setup.chainId } },
  });
  return {
    publicConfiguration: configuration,
    engineDeployment: (config: { engines: Record<number, unknown> }, engine: number) =>
      config.engines[engine] ?? null,
  };
});
vi.mock("./shared", async (importOriginal) => {
  const actual = await importOriginal<typeof import("./shared")>();
  return {
    ...actual,
    usePersistedTransactionState: () => {
      const [state, setState] = useState<TransactionState>({ status: "validating" });
      return { state, setState, ready: true };
    },
  };
});

const { StockConvertPanel } = await import("./stock-convert-panel");

/** Quotes `rate` output per input for the reference amount and `fullRate` for the full one. */
function quoteRates(rate: bigint, fullRate: bigint) {
  simulateContract.mockImplementation(async ({ args }: { args: [{ exactAmount: bigint }] }) => {
    const amount = args[0].exactAmount;
    const out = amount >= 10n ** 18n ? amount * fullRate : amount * rate;
    return { result: [out, 0n] };
  });
}

beforeEach(() => {
  setup.chainId = 4663;
  simulateContract.mockReset();
  vi.stubEnv("NEXT_PUBLIC_STOCK_ROUTING", "1");
});

afterEach(() => {
  cleanup();
  vi.unstubAllEnvs();
});

describe("StockConvertPanel", () => {
  it("renders nothing while the flag is off or off mainnet", () => {
    vi.stubEnv("NEXT_PUBLIC_STOCK_ROUTING", "");
    const off = render(<StockConvertPanel />);
    expect(off.container.innerHTML).toBe("");
    off.unmount();
    vi.stubEnv("NEXT_PUBLIC_STOCK_ROUTING", "1");
    setup.chainId = 46630;
    const testnet = render(<StockConvertPanel />);
    expect(testnet.container.innerHTML).toBe("");
  });

  it("lists the reviewed assets and quotes the multi-hop route with its price impact", async () => {
    quoteRates(10n, 10n);
    render(<StockConvertPanel />);
    expect(screen.getByRole("heading", { name: "Convert stock tokens" })).toBeTruthy();
    const select = screen.getByLabelText("Stock token") as HTMLSelectElement;
    expect([...select.options].map((option) => option.value)).toEqual(["NVDA", "AAPL", "TSLA"]);
    fireEvent.change(select, { target: { value: "AAPL" } });
    fireEvent.change(screen.getByLabelText("ETH input"), { target: { value: "1" } });
    await waitFor(() => expect(screen.getByText(/Route output/)).toBeTruthy());
    const call = simulateContract.mock.calls[0]![0] as {
      address: string;
      functionName: string;
      args: [{ exactCurrency: string; path: { intermediateCurrency: string; fee: number }[] }];
    };
    expect(call.address).toBe(v4.v4Quoter);
    expect(call.functionName).toBe("quoteExactInput");
    expect(call.args[0].exactCurrency).toBe("0x0000000000000000000000000000000000000000");
    expect(call.args[0].path.map((hop) => hop.fee)).toEqual([500, 3000]);
    expect(screen.getByText("0.00%")).toBeTruthy();
    expect(screen.getByText("2 pools; pool fees are included in this quote.")).toBeTruthy();
  });

  it("refuses to review a conversion above the price-impact limit", async () => {
    // The full amount gets 8 per input against a reference of 10: 20% impact.
    quoteRates(10n, 8n);
    render(<StockConvertPanel />);
    fireEvent.change(screen.getByLabelText("ETH input"), { target: { value: "1" } });
    await waitFor(() =>
      expect(
        screen.getByText("Price impact is above 15%. This conversion cannot be reviewed."),
      ).toBeTruthy(),
    );
    expect(
      (screen.getByRole("button", { name: "Review conversion" }) as HTMLButtonElement).disabled,
    ).toBe(true);
  });
});
