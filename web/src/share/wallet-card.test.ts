import { afterEach, describe, expect, it, vi } from "vitest";
import { CARD_REVALIDATE_SECONDS } from "./card-data";
import { GENERIC_FAILURE_MAX_AGE_SECONDS, renderWalletCard } from "./wallet-card";

/** Blocks every outbound request except the renderer's own data: URLs (its wasm). */
function blockNetwork() {
  const realFetch = globalThis.fetch;
  const blocked = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    if (String(input).startsWith("data:")) return realFetch(input, init);
    throw new Error(`outbound request blocked: ${String(input)}`);
  });
  vi.stubGlobal("fetch", blocked);
  return () =>
    blocked.mock.calls.map((call) => String(call[0])).filter((url) => !url.startsWith("data:"));
}

async function png(response: Response) {
  const bytes = new Uint8Array(await response.arrayBuffer());
  const view = new DataView(bytes.buffer);
  return {
    signature: Array.from(bytes.subarray(0, 4)),
    width: view.getUint32(16),
    height: view.getUint32(20),
  };
}

describe("renderWalletCard", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it("renders the wallet-kind card through ImageResponse with no outbound request", async () => {
    const outbound = blockNetwork();
    const response = await renderWalletCard({
      kind: "wallet",
      address: "0x00000000000000000000000000000000000000ab",
      realizedPnlWei: "-250000000000000000",
      portfolioValueWei: "2000000000000000000",
      topSymbols: ["AAA", "BBB"],
    });
    expect(response.headers.get("Content-Type")).toBe("image/png");
    expect(await png(response)).toEqual({
      signature: [0x89, 0x50, 0x4e, 0x47],
      width: 1200,
      height: 630,
    });
    expect(outbound()).toEqual([]);
    expect(response.headers.get("Cache-Control")).toBe(
      `public, s-maxage=${CARD_REVALIDATE_SECONDS}, stale-while-revalidate=${CARD_REVALIDATE_SECONDS}`,
    );
  });

  it("gives a generic card caused by an API failure a short cache lifetime", async () => {
    expect(GENERIC_FAILURE_MAX_AGE_SECONDS).toBe(30);
    const failed = await renderWalletCard({ kind: "generic", apiFailure: true });
    expect(failed.headers.get("Cache-Control")).toBe("public, s-maxage=30");
    expect((await png(failed)).width).toBe(1200);
    // An invalid address is a stable answer: the normal lifetime.
    const stable = await renderWalletCard({ kind: "generic" });
    expect(stable.headers.get("Cache-Control")).toContain(`s-maxage=${CARD_REVALIDATE_SECONDS}`);
  });
});
