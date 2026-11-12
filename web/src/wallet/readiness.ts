"use client";

import { usePrivy } from "@privy-io/react-auth";
import { useAccount, useChainId, useSwitchChain } from "wagmi";
import { useEffect, useState } from "react";
import { isAnvilWalletFixture, publicConfiguration } from "@/config/public";
import type { TransactionReadiness } from "@/transactions";

export type WalletReadinessStatus =
  "configuration-unavailable" | "provider-loading" | "disconnected" | "wrong-chain" | "ready";
export type LinkedWalletState =
  | "configuration-unavailable"
  | "provider-loading"
  | "unlinked"
  | "connected-unlinked"
  | "linked"
  | "linked-mismatch";
export type LinkedWallet = { address: `0x${string}`; kind: "wallet" | "smart_wallet" };
type ReadinessProvider = {
  request: (args: { method: string }) => Promise<unknown>;
  on?: (event: string, listener: (value: unknown) => void) => void;
  removeListener?: (event: string, listener: (value: unknown) => void) => void;
};

export type WalletReadiness = {
  status: WalletReadinessStatus;
  linkedWalletState: LinkedWalletState;
  linkedWallets: readonly LinkedWallet[];
  /** Informational Privy proof only. Creator authorization remains server-side. */
  creatorAuthorization: "server-required";
  address: `0x${string}` | undefined;
  chainId: number | undefined;
  supportedChainId: number | null;
  authenticated: boolean;
  providerReady: boolean;
  configurationReady: boolean;
  selectedAccountVerified: boolean;
  transactionReadiness: TransactionReadiness;
  switchNetwork: () => Promise<{ ok: true } | { ok: false; error: "switch-failed" }>;
};

export function getWalletStatus(args: {
  providerReady: boolean;
  configurationReady: boolean;
  address?: `0x${string}`;
  chainId?: number;
  supportedChainId: number | null;
}): WalletReadinessStatus {
  if (!args.configurationReady || !args.supportedChainId) return "configuration-unavailable";
  if (!args.providerReady) return "provider-loading";
  if (!args.address || !args.chainId) return "disconnected";
  return args.chainId === args.supportedChainId ? "ready" : "wrong-chain";
}

export function getLinkedWalletAddresses(
  user: { linkedAccounts?: readonly { type?: string; address?: string }[] } | null | undefined,
): readonly LinkedWallet[] {
  return (user?.linkedAccounts ?? []).flatMap((account) => {
    if (
      (account.type !== "wallet" && account.type !== "smart_wallet") ||
      !account.address ||
      !/^0x[0-9a-fA-F]{40}$/.test(account.address)
    )
      return [];
    return [{ address: account.address.toLowerCase() as `0x${string}`, kind: account.type }];
  });
}

export function getLinkedWalletState(args: {
  configurationReady: boolean;
  providerReady: boolean;
  authenticated: boolean;
  address?: `0x${string}`;
  linkedWallets: readonly LinkedWallet[];
}): LinkedWalletState {
  if (!args.configurationReady) return "configuration-unavailable";
  if (!args.providerReady) return "provider-loading";
  if (!args.authenticated || args.linkedWallets.length === 0)
    return args.address ? "connected-unlinked" : "unlinked";
  return args.address !== undefined &&
    args.linkedWallets.some((wallet) => wallet.address === args.address?.toLowerCase())
    ? "linked"
    : "linked-mismatch";
}

export function deriveSelectedAccountVerified(
  status: WalletReadinessStatus,
  linkedWalletState: LinkedWalletState,
): boolean {
  return status === "ready" && linkedWalletState === "linked";
}

export async function switchToSupportedChain(
  chainId: number | null,
  switchChain: ((args: { chainId: number }) => Promise<unknown>) | undefined,
): Promise<{ ok: true } | { ok: false; error: "switch-failed" }> {
  if (!chainId || !switchChain) return { ok: false, error: "switch-failed" };
  try {
    await switchChain({ chainId });
    return { ok: true };
  } catch {
    return { ok: false, error: "switch-failed" };
  }
}

export function useWalletReadiness(): WalletReadiness {
  const configuration = publicConfiguration();
  const { ready: privyReady, authenticated, user } = usePrivy();
  const { address, isConnected } = useAccount();
  const wagmiChainId = useChainId();
  const [providerChainId, setProviderChainId] = useState<number | undefined>(wagmiChainId);
  useEffect(() => {
    const provider = (window as Window & { ethereum?: ReadinessProvider }).ethereum;
    if (!provider) return;
    const sync = () => {
      void provider.request({ method: "eth_chainId" }).then((value) => {
        if (typeof value === "string" && /^0x[0-9a-f]+$/i.test(value))
          setProviderChainId(Number.parseInt(value, 16));
      });
    };
    const onChainChanged = (value: unknown) => {
      if (typeof value === "string" && /^0x[0-9a-f]+$/i.test(value))
        setProviderChainId(Number.parseInt(value, 16));
    };
    sync();
    provider.on?.("chainChanged", onChainChanged);
    return () => provider.removeListener?.("chainChanged", onChainChanged);
  }, []);
  const chainId = providerChainId ?? wagmiChainId;
  const { switchChainAsync } = useSwitchChain();
  const configurationReady = configuration.status === "ready";
  const e2eFixture = isAnvilWalletFixture(configuration);
  const selectedAddress = isConnected ? address : undefined;
  const linkedWallets =
    e2eFixture && selectedAddress
      ? [{ address: selectedAddress.toLowerCase() as `0x${string}`, kind: "wallet" as const }]
      : getLinkedWalletAddresses(user);
  const providerReady = e2eFixture ? true : privyReady;
  const isAuthenticated = e2eFixture ? Boolean(selectedAddress) : authenticated;
  const status = getWalletStatus({
    providerReady,
    configurationReady,
    address: selectedAddress,
    chainId: isConnected ? chainId : undefined,
    supportedChainId: configuration.chainId,
  });
  const linkedWalletState = getLinkedWalletState({
    configurationReady,
    providerReady,
    authenticated: isAuthenticated,
    address: selectedAddress,
    linkedWallets,
  });
  const selectedAccountVerified = deriveSelectedAccountVerified(status, linkedWalletState);
  const switchNetwork = async () => {
    const result = await switchToSupportedChain(configuration.chainId, switchChainAsync);
    if (result.ok) {
      const provider = (window as Window & { ethereum?: ReadinessProvider }).ethereum;
      if (provider) {
        try {
          const value = await provider.request({ method: "eth_chainId" });
          if (typeof value === "string" && /^0x[0-9a-f]+$/i.test(value))
            setProviderChainId(Number.parseInt(value, 16));
        } catch {
          // The chainChanged event remains the authoritative update path.
        }
      }
    }
    return result;
  };
  return {
    status,
    linkedWalletState,
    linkedWallets,
    creatorAuthorization: "server-required",
    address: selectedAddress,
    chainId: isConnected ? chainId : undefined,
    supportedChainId: configuration.chainId,
    authenticated: isAuthenticated,
    providerReady,
    configurationReady,
    selectedAccountVerified,
    transactionReadiness: {
      providerReady,
      configurationReady,
      walletConnected: selectedAddress !== undefined,
      chainSupported: status === "ready",
      selectedAccountVerified,
      linkedWalletState,
    },
    switchNetwork,
  };
}
