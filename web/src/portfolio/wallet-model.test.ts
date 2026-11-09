import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { LiveEvent } from "@/api/types";
import {
  WALLET_REFETCH_COALESCE_MS,
  WALLET_REFETCH_FOLLOW_UP_MS,
  createCoalescer,
  createLiveRefetch,
  reloadLoadedPages,
  formatSignedEth,
  isWalletTrade,
  pnlTone,
  pnlUsd,
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
  it("never renders a signed zero below display precision", () => {
    expect(formatSignedEth("1")).toBe("+<0.0001 ETH");
    expect(formatSignedEth("-99999999999999")).toBe("−<0.0001 ETH");
    expect(formatSignedEth("100000000000000")).toBe("+0.0001 ETH");
    expect(formatSignedEth("-100000000000000")).toBe("−0.0001 ETH");
    expect(pnlTone("-1")).toBe("loss");
  });
  it("keeps precision beyond float range", () => {
    expect(formatSignedEth("123456789012345678901234567890")).toBe("+123,456,789,012.3456 ETH");
  });
  it("falls back for malformed input", () => {
    expect(formatSignedEth("1.5")).toBe("Unavailable");
    expect(pnlTone("garbage")).toBe("flat");
  });
  it("splits a PnL into the USD magnitude and its sign", () => {
    expect(pnlUsd("-500000000000000000")).toEqual({ wei: "500000000000000000", negative: true });
    expect(pnlUsd("200000000000000000")).toEqual({ wei: "200000000000000000", negative: false });
    expect(pnlUsd("0")).toEqual({ wei: "0", negative: false });
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

describe("createLiveRefetch", () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => vi.useRealTimers());

  it("refetches after the coalesce window and once more 3 s later", () => {
    expect(WALLET_REFETCH_FOLLOW_UP_MS).toBe(3_000);
    const run = vi.fn();
    const refetch = createLiveRefetch(run);
    refetch.trigger();
    refetch.trigger();
    vi.advanceTimersByTime(WALLET_REFETCH_COALESCE_MS - 1);
    expect(run).toHaveBeenCalledTimes(0);
    vi.advanceTimersByTime(1);
    expect(run).toHaveBeenCalledTimes(1);
    vi.advanceTimersByTime(WALLET_REFETCH_FOLLOW_UP_MS - 1);
    expect(run).toHaveBeenCalledTimes(1);
    vi.advanceTimersByTime(1);
    expect(run).toHaveBeenCalledTimes(2);
    vi.advanceTimersByTime(60_000);
    expect(run).toHaveBeenCalledTimes(2);
  });

  it("keeps a single delayed refetch when triggers keep arriving", () => {
    const run = vi.fn();
    const refetch = createLiveRefetch(run);
    refetch.trigger();
    vi.advanceTimersByTime(500); // immediate run #1, follow-up due at 3500
    vi.advanceTimersByTime(1_000);
    refetch.trigger();
    vi.advanceTimersByTime(500); // immediate run #2 at 2000, follow-up moves to 5000
    expect(run).toHaveBeenCalledTimes(2);
    vi.advanceTimersByTime(1_500); // 3500: the old follow-up no longer fires
    expect(run).toHaveBeenCalledTimes(2);
    vi.advanceTimersByTime(1_500); // 5000
    expect(run).toHaveBeenCalledTimes(3);
    vi.advanceTimersByTime(60_000);
    expect(run).toHaveBeenCalledTimes(3);
  });

  it("cancel drops both the pending and the delayed refetch", () => {
    const run = vi.fn();
    const refetch = createLiveRefetch(run);
    refetch.trigger();
    refetch.cancel();
    vi.advanceTimersByTime(10_000);
    expect(run).not.toHaveBeenCalled();
    refetch.trigger();
    vi.advanceTimersByTime(500);
    refetch.cancel();
    vi.advanceTimersByTime(10_000);
    expect(run).toHaveBeenCalledTimes(1);
  });
});

describe("reloadLoadedPages", () => {
  const rows = Array.from({ length: 60 }, (_, i) => ({ id: `r${i}` }));
  function pager(pageSize: number) {
    const cursors: Array<string | undefined> = [];
    const fetchPage = vi.fn(async (cursor: string | undefined) => {
      cursors.push(cursor);
      const start = cursor ? Number(cursor) : 0;
      const items = rows.slice(start, start + pageSize);
      const end = start + items.length;
      return { items, next_cursor: end < rows.length ? String(end) : undefined };
    });
    return { fetchPage, cursors };
  }
  const identity = (row: { id: string }) => row.id;

  it("re-requests as many pages as were loaded, keeping their rows", async () => {
    const { fetchPage, cursors } = pager(25);
    const page = await reloadLoadedPages(fetchPage, identity, 50, new AbortController().signal);
    expect(cursors).toEqual([undefined, "25"]);
    expect(page.items.map(identity)).toEqual(rows.slice(0, 50).map(identity));
    expect(page.next).toBe("50");
  });

  it("loads only page one when nothing more was loaded", async () => {
    const { fetchPage, cursors } = pager(25);
    const page = await reloadLoadedPages(fetchPage, identity, 0, new AbortController().signal);
    expect(cursors).toEqual([undefined]);
    expect(page.items).toHaveLength(25);
    expect(page.next).toBe("25");
  });

  it("stops at the end of the list and drops repeated rows", async () => {
    const fetchPage = vi.fn(async (cursor: string | undefined) =>
      cursor
        ? { items: [{ id: "b" }, { id: "c" }], next_cursor: undefined }
        : { items: [{ id: "a" }, { id: "b" }], next_cursor: "x" },
    );
    const page = await reloadLoadedPages(fetchPage, identity, 100, new AbortController().signal);
    expect(page.items.map(identity)).toEqual(["a", "b", "c"]);
    expect(page.next).toBeNull();
    expect(fetchPage).toHaveBeenCalledTimes(2);
  });

  it("stops when a page adds no new rows instead of looping on its cursor", async () => {
    const fetchPage = vi.fn(async () => ({ items: [{ id: "a" }], next_cursor: "same" }));
    const page = await reloadLoadedPages(fetchPage, identity, 100, new AbortController().signal);
    expect(page.items.map(identity)).toEqual(["a"]);
    expect(fetchPage).toHaveBeenCalledTimes(2);
  });

  it("stops following cursors once aborted", async () => {
    const controller = new AbortController();
    const fetchPage = vi.fn(async () => {
      controller.abort();
      return { items: [{ id: "a" }], next_cursor: "x" };
    });
    await expect(reloadLoadedPages(fetchPage, identity, 100, controller.signal)).rejects.toThrow();
    expect(fetchPage).toHaveBeenCalledTimes(1);
  });
});
