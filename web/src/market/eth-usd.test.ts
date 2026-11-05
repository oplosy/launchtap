import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { EthUsdReference } from "@/api/client";
import { createEthUsdStore } from "./eth-usd";
import { UsdLine, UsdLineView } from "./usd-line";

function reference(price: string | null): EthUsdReference {
  return {
    price,
    provider_updated_at: null,
    fetched_at: null,
    source: price ? "coingecko" : null,
    attribution_url: price ? "https://www.coingecko.com" : null,
  };
}

function fakeDocument() {
  const target = new EventTarget();
  const doc = {
    visibilityState: "visible" as "visible" | "hidden",
    addEventListener: target.addEventListener.bind(target),
    removeEventListener: target.removeEventListener.bind(target),
    hide() {
      doc.visibilityState = "hidden";
      target.dispatchEvent(new Event("visibilitychange"));
    },
    show() {
      doc.visibilityState = "visible";
      target.dispatchEvent(new Event("visibilitychange"));
    },
  };
  return doc;
}

describe("eth usd store", () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => vi.useRealTimers());

  it("exposes the price and attribution url", async () => {
    const fetcher = vi.fn().mockResolvedValue(reference("3456.12"));
    const store = createEthUsdStore(fetcher, fakeDocument());
    const stop = store.subscribe(() => undefined);
    await vi.advanceTimersByTimeAsync(0);
    expect(store.getSnapshot()).toEqual({
      price: "3456.12",
      attributionUrl: "https://www.coingecko.com",
    });
    stop();
  });

  it("returns a null price when the API returns null or fails", async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValueOnce(reference(null))
      .mockRejectedValueOnce(new Error("boom"));
    const store = createEthUsdStore(fetcher, fakeDocument());
    const stop = store.subscribe(() => undefined);
    await vi.advanceTimersByTimeAsync(0);
    expect(store.getSnapshot().price).toBeNull();
    await vi.advanceTimersByTimeAsync(60_000);
    expect(store.getSnapshot()).toEqual({ price: null, attributionUrl: null });
    stop();
  });

  it("drops a stale price when requests hang", async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValueOnce(reference("1"))
      .mockImplementation(() => new Promise(() => undefined));
    const store = createEthUsdStore(fetcher, fakeDocument());
    const stop = store.subscribe(() => undefined);
    await vi.advanceTimersByTimeAsync(0);
    expect(store.getSnapshot().price).toBe("1");
    await vi.advanceTimersByTimeAsync(120_000 + 60_000);
    expect(store.getSnapshot()).toEqual({ price: null, attributionUrl: null });
    stop();
  });

  it("shares one fetch between subscribers and polls every 60 seconds", async () => {
    const fetcher = vi.fn().mockResolvedValue(reference("1"));
    const store = createEthUsdStore(fetcher, fakeDocument());
    const stopA = store.subscribe(() => undefined);
    const stopB = store.subscribe(() => undefined);
    await vi.advanceTimersByTimeAsync(0);
    expect(fetcher).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(60_000);
    expect(fetcher).toHaveBeenCalledTimes(2);
    stopA();
    stopB();
    await vi.advanceTimersByTimeAsync(120_000);
    expect(fetcher).toHaveBeenCalledTimes(2);
  });

  it("does not poll while hidden and refreshes when visible again", async () => {
    const doc = fakeDocument();
    const fetcher = vi.fn().mockResolvedValue(reference("1"));
    const store = createEthUsdStore(fetcher, doc);
    const stop = store.subscribe(() => undefined);
    await vi.advanceTimersByTimeAsync(0);
    expect(fetcher).toHaveBeenCalledTimes(1);
    doc.hide();
    await vi.advanceTimersByTimeAsync(180_000);
    expect(fetcher).toHaveBeenCalledTimes(1);
    doc.show();
    await vi.advanceTimersByTimeAsync(0);
    expect(fetcher).toHaveBeenCalledTimes(2);
    stop();
  });
});

describe("UsdLine", () => {
  it("renders nothing when the price is null or the amount is invalid", () => {
    expect(renderToStaticMarkup(createElement(UsdLineView, { wei: "10", price: null }))).toBe("");
    expect(renderToStaticMarkup(createElement(UsdLineView, { wei: "x", price: "1" }))).toBe("");
    expect(renderToStaticMarkup(createElement(UsdLine, { wei: "10" }))).toBe("");
  });
  it("renders nothing for non-canonical wei strings", () => {
    for (const wei of ["", " 1", "0x10", "01", "1.5", "-1"])
      expect(renderToStaticMarkup(createElement(UsdLineView, { wei, price: "1" }))).toBe("");
  });
  it("renders the formatted USD value", () => {
    const html = renderToStaticMarkup(
      createElement(UsdLineView, { wei: (10n ** 18n).toString(), price: "3456.12" }),
    );
    expect(html).toContain("$3,456.12");
  });
});
