import { afterEach, describe, expect, it, vi } from "vitest";
import { publicConfiguration } from "@/config/public";
import { createWeb3Config, rpcTransport } from "./config";

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("wallet configuration", () => {
  it("does not construct a provider config when public configuration is unavailable", () => {
    expect(createWeb3Config(publicConfiguration({}))).toBeNull();
  });

  it("sends reads issued in the same tick as one batched JSON-RPC request", async () => {
    const bodies: unknown[] = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (_url: string, init: RequestInit) => {
        const body = JSON.parse(String(init.body)) as { id: number }[];
        bodies.push(body);
        return new Response(
          JSON.stringify(
            body.map((request) => ({ jsonrpc: "2.0", id: request.id, result: "0x1" })),
          ),
          { headers: { "Content-Type": "application/json" } },
        );
      }),
    );
    const transport = rpcTransport("http://rpc.test")({ chain: undefined, retryCount: 0 });

    await Promise.all([
      transport.request({ method: "eth_blockNumber" }),
      transport.request({ method: "eth_chainId" }),
    ]);

    expect(bodies).toHaveLength(1);
    expect(bodies[0]).toHaveLength(2);
  });
});
