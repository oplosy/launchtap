import { createConfig as createPrivyConfig } from "@privy-io/wagmi";
import { createConfig as createWagmiConfig, type Config } from "wagmi";
import { injected } from "wagmi/connectors";
import { defineChain, http, type Chain, type HttpTransport } from "viem";
import { isAnvilWalletFixture, type PublicConfiguration } from "@/config/public";

export function supportedChain(configuration: PublicConfiguration): Chain | null {
  if (
    configuration.status !== "ready" ||
    !configuration.chainId ||
    !configuration.rpcUrl ||
    !configuration.deployment
  )
    return null;
  return defineChain({
    id: configuration.chainId,
    name: configuration.deployment.name,
    nativeCurrency: { name: "Ether", symbol: "ETH", decimals: 18 },
    rpcUrls: { default: { http: [configuration.rpcUrl] } },
    blockExplorers: configuration.deployment.explorerBase
      ? { default: { name: "Explorer", url: configuration.deployment.explorerBase } }
      : undefined,
  });
}

/**
 * The read transport for the configured RPC. JSON-RPC batching sends reads issued in the same
 * tick (for example a Promise.all of contract reads) as one HTTP request instead of one each.
 */
export function rpcTransport(rpcUrl: string): HttpTransport {
  return http(rpcUrl, { batch: true });
}

export function createWeb3Config(configuration: PublicConfiguration): {
  chain: Chain;
  config: Config;
} | null {
  const chain = supportedChain(configuration);
  if (!chain || !configuration.rpcUrl) return null;
  const config = isAnvilWalletFixture(configuration)
    ? createWagmiConfig({
        chains: [chain] as const,
        transports: { [chain.id]: rpcTransport(configuration.rpcUrl) },
        connectors: [injected()],
      })
    : createPrivyConfig({
        chains: [chain] as const,
        transports: { [chain.id]: rpcTransport(configuration.rpcUrl) },
      });
  return { chain, config };
}
