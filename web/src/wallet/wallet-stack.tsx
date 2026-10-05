"use client";

import { PrivyProvider } from "@privy-io/react-auth";
import { WagmiProvider as PrivyWagmiProvider } from "@privy-io/wagmi";
import { WagmiProvider } from "wagmi";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useState, type PropsWithChildren } from "react";
import { publicConfiguration } from "@/config/public";
import { createWeb3Config } from "./config";
import { PrivyWalletConnectionBridge, WagmiWalletConnectionBridge } from "./connection";

/**
 * Privy, wagmi, and the wallet bridges. This module is loaded on demand by `Providers` so routes
 * that never sign do not download the wallet SDKs up front.
 */
export function WalletStack({
  children,
  openOnMount = false,
}: PropsWithChildren<{ openOnMount?: boolean }>) {
  const configuration = publicConfiguration();
  const web3 = createWeb3Config(configuration);
  const [queryClient] = useState(() => new QueryClient());

  if (!web3) return children;
  if (configuration.deploymentId === "task6-anvil" && !configuration.privyAppId)
    return (
      <WagmiProvider config={web3.config}>
        <QueryClientProvider client={queryClient}>
          <WagmiWalletConnectionBridge openOnMount={openOnMount}>
            {children}
          </WagmiWalletConnectionBridge>
        </QueryClientProvider>
      </WagmiProvider>
    );
  if (!configuration.privyAppId) return children;
  if (configuration.deploymentId === "task6-anvil")
    return (
      <WagmiProvider config={web3.config}>
        <PrivyProvider appId={configuration.privyAppId}>
          <QueryClientProvider client={queryClient}>
            <WagmiWalletConnectionBridge openOnMount={openOnMount}>
              {children}
            </WagmiWalletConnectionBridge>
          </QueryClientProvider>
        </PrivyProvider>
      </WagmiProvider>
    );
  return (
    <PrivyProvider appId={configuration.privyAppId}>
      <QueryClientProvider client={queryClient}>
        <PrivyWagmiProvider config={web3.config}>
          <PrivyWalletConnectionBridge openOnMount={openOnMount}>
            {children}
          </PrivyWalletConnectionBridge>
        </PrivyWagmiProvider>
      </QueryClientProvider>
    </PrivyProvider>
  );
}
