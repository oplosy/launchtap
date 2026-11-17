// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { TransactionState } from "@/transactions";
import { act, renderHook, waitFor } from "@testing-library/react";
import { readPersistedTransaction, writePersistedTransaction } from "@/transaction-storage";
import { observeFreshSnapshot, usePersistedTransactionState } from "./transaction-persistence";

const hash = `0x${"ab".repeat(32)}` as const;
const base = "https://api.example";

function canonical(finality: string, txHash: string = hash) {
  return new Response(
    JSON.stringify({ finality, events: [{ kind: "trade", tx_hash: txHash, finality }] }),
    { status: 200 },
  );
}

describe("observeFreshSnapshot", () => {
  let fetchMock: ReturnType<typeof vi.fn>;
  beforeEach(() => {
    vi.useFakeTimers();
    fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
  });
  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  function observe(state: Pick<TransactionState, "hash" | "status">, isCurrent = () => true) {
    const states: TransactionState[] = [];
    const stop = observeFreshSnapshot(
      base,
      "0xtoken",
      "trade",
      state,
      (next) => states.push(next),
      isCurrent,
    );
    return { states, stop };
  }

  it("does nothing without an API base URL", () => {
    const setState = vi.fn();
    const stop = observeFreshSnapshot(
      null,
      null,
      "trade",
      { hash, status: "submitted" },
      setState,
      () => true,
    );
    stop();
    expect(fetchMock).not.toHaveBeenCalled();
    expect(setState).not.toHaveBeenCalled();
  });

  it("advances to the indexed finality and keeps polling until finalized", async () => {
    fetchMock
      .mockResolvedValueOnce(canonical("safe"))
      .mockResolvedValueOnce(canonical("finalized"));
    const { states, stop } = observe({ hash, status: "submitted" });
    await vi.waitFor(() => expect(states).toHaveLength(1));
    expect(states[0]).toMatchObject({ status: "safe", canonicalRefreshUnavailable: false });
    expect(String(fetchMock.mock.calls[0]?.[0])).toBe(`${base}/v1/transactions/${hash}`);

    await vi.advanceTimersByTimeAsync(500);
    await vi.waitFor(() => expect(states).toHaveLength(2));
    expect(states[1]?.status).toBe("finalized");

    // Finalized: no further polling.
    await vi.advanceTimersByTimeAsync(120_000);
    expect(fetchMock).toHaveBeenCalledTimes(2);
    stop();
  });

  it("backs off between polls while the record is still indexing", async () => {
    fetchMock.mockImplementation(async () => canonical("indexed", "0x" + "cd".repeat(32)));
    const { stop } = observe({ hash, status: "submitted" });
    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
    await vi.advanceTimersByTimeAsync(499);
    expect(fetchMock).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(1);
    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2));
    await vi.advanceTimersByTimeAsync(1_000);
    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(3));
    stop();
    await vi.advanceTimersByTimeAsync(120_000);
    expect(fetchMock).toHaveBeenCalledTimes(3);
  });

  it("regresses a canonical transaction whose record disappeared and signals a reorg", async () => {
    fetchMock.mockResolvedValue(
      new Response(JSON.stringify({ type: "urn:launchpad:problem:not_found" }), { status: 404 }),
    );
    const reorgs: Event[] = [];
    window.addEventListener("launchpad:canonical-reorg", (event) => reorgs.push(event));
    const { states, stop } = observe({ hash, status: "safe" });
    await vi.waitFor(() => expect(states).toHaveLength(1));
    expect(states[0]?.status).toBe("indexing");
    expect(reorgs).toHaveLength(1);
    expect((reorgs[0] as CustomEvent).detail).toEqual({ tokenAddress: "0xtoken" });
    stop();
  });

  it("marks the refresh unavailable on a transport failure without regressing", async () => {
    fetchMock.mockRejectedValue(new TypeError("network down"));
    const { states, stop } = observe({ hash, status: "safe" });
    await vi.waitFor(() => expect(states).toHaveLength(1));
    expect(states[0]).toMatchObject({ status: "safe", canonicalRefreshUnavailable: true });
    stop();
  });

  it("drops results once the scope is no longer current", async () => {
    let current = true;
    fetchMock.mockImplementation(async () => {
      current = false;
      return canonical("finalized");
    });
    const { states } = observe({ hash, status: "submitted" }, () => current);
    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
    await vi.advanceTimersByTimeAsync(60_000);
    expect(states).toHaveLength(0);
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it("refreshes on focus and stops listening after stop", async () => {
    fetchMock.mockImplementation(async () => canonical("indexed", "0x" + "cd".repeat(32)));
    const { stop } = observe({ hash, status: "submitted" });
    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
    window.dispatchEvent(new Event("focus"));
    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2));
    stop();
    window.dispatchEvent(new Event("focus"));
    window.dispatchEvent(new Event("online"));
    await vi.advanceTimersByTimeAsync(10);
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });
});

describe("usePersistedTransactionState", () => {
  const account = "0x00000000000000000000000000000000000000a1" as const;
  const token = "0x00000000000000000000000000000000000000b2" as const;
  const context = { chainId: 31337, account, tokenAddress: token, action: "trade" as const };

  beforeEach(() => window.localStorage.clear());
  afterEach(() => vi.unstubAllGlobals());

  it("stays not ready while disconnected", () => {
    const { result } = renderHook(() =>
      usePersistedTransactionState("trade", 31337, undefined, token, null),
    );
    expect(result.current.ready).toBe(false);
    expect(result.current.state.status).toBe("disconnected");
  });

  it("restores the persisted transaction of its scope after mount", async () => {
    writePersistedTransaction(context, { hash, status: "safe" }, window.localStorage);
    const { result } = renderHook(() =>
      usePersistedTransactionState("trade", 31337, account, token, null),
    );
    await waitFor(() => expect(result.current.ready).toBe(true));
    expect(result.current.state).toMatchObject({ status: "safe", hash });
  });

  it("persists a submitted transaction and starts fresh in another scope", async () => {
    const { result, rerender } = renderHook(
      ({ wallet }) => usePersistedTransactionState("trade", 31337, wallet, token, null),
      { initialProps: { wallet: account as string } },
    );
    await waitFor(() => expect(result.current.ready).toBe(true));
    act(() => result.current.setState({ status: "submitted", hash }));
    expect(result.current.state).toMatchObject({ status: "submitted", hash });
    expect(readPersistedTransaction(context, window.localStorage)).toMatchObject({
      hash,
      status: "submitted",
    });

    rerender({ wallet: "0x00000000000000000000000000000000000000c3" });
    await waitFor(() => expect(result.current.ready).toBe(true));
    expect(result.current.state.hash).toBeUndefined();
  });

  it("observes the canonical record of a restored transaction, but not of an approval", async () => {
    const fetchMock = vi.fn<typeof fetch>(async () => canonical("finalized"));
    vi.stubGlobal("fetch", fetchMock);
    writePersistedTransaction(context, { hash, status: "submitted" }, window.localStorage);
    const { result } = renderHook(() =>
      usePersistedTransactionState("trade", 31337, account, token, base),
    );
    await waitFor(() => expect(result.current.state.status).toBe("finalized"));
    expect(String(fetchMock.mock.calls[0]?.[0])).toBe(`${base}/v1/transactions/${hash}`);

    fetchMock.mockClear();
    writePersistedTransaction(
      { ...context, action: "approval" },
      { hash, status: "submitted" },
      window.localStorage,
    );
    const approval = renderHook(() =>
      usePersistedTransactionState("approval", 31337, account, token, base),
    );
    await waitFor(() => expect(approval.result.current.ready).toBe(true));
    expect(fetchMock).not.toHaveBeenCalled();
  });
});
