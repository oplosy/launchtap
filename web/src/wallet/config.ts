import { createConfig as createPrivyConfig } from "@privy-io/wagmi";
import { createConfig as createWagmiConfig, type Config } from "wagmi";
import { injected } from "wagmi/connectors";
import { defineChain, http, type Chain } from "viem";
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

export function createWeb3Config(configuration: PublicConfiguration): {
  chain: Chain;
  config: Config;
} | null {
  const chain = supportedChain(configuration);
  if (!chain || !configuration.rpcUrl) return null;
  const config = isAnvilWalletFixture(configuration)
    ? createWagmiConfig({
        chains: [chain] as const,
        transports: { [chain.id]: http(configuration.rpcUrl) },
        connectors: [injected()],
      })
    : createPrivyConfig({
        chains: [chain] as const,
        transports: { [chain.id]: http(configuration.rpcUrl) },
      });
  return { chain, config };
}
