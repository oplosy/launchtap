"use client";

import { createContext, useContext } from "react";
import { Wallet } from "@/components/icons";
import { Button, type ButtonProps } from "@/components/primitives";
import { shortAddress } from "@/token/address";

/**
 * Wallet state shared with the app shell. This module stays free of Privy and wagmi imports so
 * routes that do not sign can render without loading the wallet stack.
 */
export type WalletConnection = {
  open: () => void;
  disconnect: () => void;
  connected: boolean;
  pending: boolean;
  available: boolean;
  address?: string;
};

export const fallbackConnection: WalletConnection = {
  open: () => undefined,
  disconnect: () => undefined,
  connected: false,
  pending: false,
  available: false,
};

export const WalletConnectionContext = createContext<WalletConnection>(fallbackConnection);

export function useWalletConnection() {
  return useContext(WalletConnectionContext);
}

export function WalletConnectButton({
  className,
  variant = "quiet",
  size = "sm",
}: {
  className?: string;
  variant?: ButtonProps["variant"];
  size?: ButtonProps["size"];
} = {}) {
  const wallet = useWalletConnection();
  const label = wallet.pending
    ? "Connecting wallet"
    : wallet.connected && wallet.address
      ? shortAddress(wallet.address)
      : wallet.available
        ? "Connect wallet"
        : "Wallet unavailable";
  return (
    <Button
      variant={variant}
      size={size}
      onClick={wallet.open}
      disabled={!wallet.available}
      loading={wallet.pending}
      className={className ?? "navbar-wallet"}
      aria-label={label}
      title={label}
    >
      <Wallet size={17} aria-hidden="true" /> <span>{label}</span>
    </Button>
  );
}
