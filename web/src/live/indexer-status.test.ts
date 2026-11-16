import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { StatusResponse } from "@/api/client";
import {
  createIndexerStatusStore,
  describeDelay,
  INDEXER_STATUS_POLL_INTERVAL_MS,
} from "./indexer-status";
import { StaleDataBannerView } from "./stale-data-banner";

function response(stale: boolean, age: number | null): StatusResponse {
  return {
    chain_id: 46630,
    deployment_id: "testnet",
    indexer: {
      stale,
      stale_after_seconds: 120,
      heartbeat_at: age === null ? null : "2026-11-16T12:00:00Z",
      heartbeat_age_seconds: age,
      observed_block: 10,
      safe_block: 9,
      finalized_block: 8,
    },
  };
}

describe("indexer status store", () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => vi.useRealTimers());

  it("follows the server's stale verdict across polls", async () => {
    const fetcher = vi
      .fn<(signal: AbortSignal) => Promise<StatusResponse>>()
      .mockResolvedValueOnce(response(true, 400))
      .mockResolvedValueOnce(response(false, 1));
    const store = createIndexerStatusStore(fetcher, null);
    const unsubscribe = store.subscribe(() => undefined);
    await vi.waitFor(() => expect(store.getSnapshot()).toEqual({ stale: true, ageSeconds: 400 }));
    await vi.advanceTimersByTimeAsync(INDEXER_STATUS_POLL_INTERVAL_MS);
    expect(store.getSnapshot()).toEqual({ stale: false, ageSeconds: null });
    unsubscribe();
  });

  it("does not claim stale data when the request fails", async () => {
    const fetcher = vi
      .fn<(signal: AbortSignal) => Promise<StatusResponse>>()
      .mockResolvedValueOnce(response(true, null))
      .mockRejectedValueOnce(new Error("offline"));
    const store = createIndexerStatusStore(fetcher, null);
    const unsubscribe = store.subscribe(() => undefined);
    await vi.waitFor(() => expect(store.getSnapshot().stale).toBe(true));
    await vi.advanceTimersByTimeAsync(INDEXER_STATUS_POLL_INTERVAL_MS);
    expect(store.getSnapshot().stale).toBe(false);
    unsubscribe();
  });

  it("stops polling after the last subscriber leaves", async () => {
    const fetcher = vi
      .fn<(signal: AbortSignal) => Promise<StatusResponse>>()
      .mockResolvedValue(response(false, 1));
    const store = createIndexerStatusStore(fetcher, null);
    store.subscribe(() => undefined)();
    await vi.advanceTimersByTimeAsync(3 * INDEXER_STATUS_POLL_INTERVAL_MS);
    expect(fetcher).toHaveBeenCalledTimes(1);
  });
});

describe("stale data banner", () => {
  it("renders only while stale", () => {
    expect(
      renderToStaticMarkup(
        createElement(StaleDataBannerView, { state: { stale: false, ageSeconds: null } }),
      ),
    ).toBe("");
    expect(
      renderToStaticMarkup(
        createElement(StaleDataBannerView, { state: { stale: true, ageSeconds: 300 } }),
      ),
    ).toContain("5 minutes ago");
    expect(
      renderToStaticMarkup(
        createElement(StaleDataBannerView, { state: { stale: true, ageSeconds: null } }),
      ),
    ).toContain("not available yet");
  });

  it("describes the delay in minutes or hours", () => {
    expect(describeDelay(null)).toBeNull();
    expect(describeDelay(30)).toBe("1 minute");
    expect(describeDelay(150)).toBe("2 minutes");
    expect(describeDelay(7300)).toBe("2 hours");
  });
});
