import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { LiveEvent } from "@/api/types";
import {
  WALLET_REFETCH_COALESCE_MS,
  createCoalescer,
  formatSignedEth,
  isWalletTrade,
  pnlTone,
  walletPath,
} from "./wallet-model";

const wallet = "0x00000000000000000000000000000000000000Ab";

function tradeEvent(trader: string | null): LiveEvent {
  return {
    event: "trade",
    data: {
      block_number: 1,
      chain_id: 1,
      deployment_id: "d",
      eth_volume: "1",
      finality: "observed",
      log_index: 0,
      name: "n",
      side: "buy",
      source: "curve",
      symbol: "S",
      time: "2026-01-01T00:00:00Z",
      token: "0x0000000000000000000000000000000000000001",
      token_volume: "1",
      trader,
      transaction_index: 0,
      tx_hash: "0x01",
    },
  };
}

describe("walletPath", () => {
  it("lowercases the address", () => {
    expect(walletPath(wallet)).toBe(`/wallet/${wallet.toLowerCase()}`);
  });
});

describe("isWalletTrade", () => {
  it("matches a mixed-case trader", () => {
    expect(isWalletTrade(tradeEvent(wallet.toUpperCase().replace("0X", "0x")), wallet)).toBe(true);
    expect(isWalletTrade(tradeEvent(wallet.toLowerCase()), wallet)).toBe(true);
  });
  it("rejects a null trader, another wallet and non-trade events", () => {
    expect(isWalletTrade(tradeEvent(null), wallet)).toBe(false);
    expect(isWalletTrade(tradeEvent("0x00000000000000000000000000000000000000cd"), wallet)).toBe(
      false,
    );
    const graduation = {
      event: "graduation",
      data: { token: "0x01" },
    } as unknown as LiveEvent;
    expect(isWalletTrade(graduation, wallet)).toBe(false);
    expect(isWalletTrade({ event: "reconnected" }, wallet)).toBe(false);
  });
});

describe("formatSignedEth and pnlTone", () => {
  it("formats gains, losses and zero with bigint math", () => {
    expect(formatSignedEth("200000000000000000")).toBe("+0.2 ETH");
    expect(formatSignedEth("-500000000000000000")).toBe("−0.5 ETH");
    expect(formatSignedEth("0")).toBe("0 ETH");
  });
  it("keeps precision beyond float range", () => {
    expect(formatSignedEth("123456789012345678901234567890")).toBe("+123,456,789,012.3456 ETH");
  });
  it("falls back for malformed input", () => {
    expect(formatSignedEth("1.5")).toBe("Unavailable");
    expect(pnlTone("garbage")).toBe("flat");
  });
  it("derives the tone from the sign", () => {
    expect(pnlTone("1")).toBe("gain");
    expect(pnlTone("-1")).toBe("loss");
    expect(pnlTone("0")).toBe("flat");
  });
});

describe("createCoalescer", () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => vi.useRealTimers());

  it("runs once for three triggers within the window", () => {
    const run = vi.fn();
    const coalescer = createCoalescer(WALLET_REFETCH_COALESCE_MS, run);
    coalescer.trigger();
    vi.advanceTimersByTime(100);
    coalescer.trigger();
    coalescer.trigger();
    vi.advanceTimersByTime(WALLET_REFETCH_COALESCE_MS);
    expect(run).toHaveBeenCalledTimes(1);
    coalescer.trigger();
    vi.advanceTimersByTime(WALLET_REFETCH_COALESCE_MS);
    expect(run).toHaveBeenCalledTimes(2);
  });

  it("cancel drops a pending run", () => {
    const run = vi.fn();
    const coalescer = createCoalescer(500, run);
    coalescer.trigger();
    coalescer.cancel();
    vi.advanceTimersByTime(1000);
    expect(run).not.toHaveBeenCalled();
  });
});
