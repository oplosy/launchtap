"use client";

import { createContext, useContext } from "react";
import { Wallet } from "@/components/icons";
import { Button, type ButtonProps } from "@/components/primitives";
import { shortAddress } from "@/token/address";

/**
 * Wallet state shared with the app shell. This module stays free of Privy and wagmi imports so
 * routes that do not sign can render without loading the wallet stack.
 */
/** Lifecycle of the lazily loaded wallet stack as seen by consumers. */
export type WalletStackState = "idle" | "loading" | "mounted" | "failed";

export type WalletConnection = {
  stack: WalletStackState;
  open: () => void;
  /** Mounts the wallet stack without opening the picker. A no-op once mounted. */
  prepare: () => void;
  disconnect: () => void;
  connected: boolean;
  pending: boolean;
  available: boolean;
  address?: string;
};

export const fallbackConnection: WalletConnection = {
  open: () => undefined,
  prepare: () => undefined,
  stack: "idle",
  disconnect: () => undefined,
  connected: false,
  pending: false,
  available: false,
};

export const WalletConnectionContext = createContext<WalletConnection>(fallbackConnection);

export function useWalletConnection() {
  return useContext(WalletConnectionContext);
}

/**
 * `compact` shortens the visible text for the navbar ("Connect", "Connecting"); the accessible
 * name keeps the full label and still starts with the visible words.
 */
export function WalletConnectButton({
  className,
  variant = "quiet",
  size = "sm",
  compact = false,
}: {
  className?: string;
  variant?: ButtonProps["variant"];
  size?: ButtonProps["size"];
  compact?: boolean;
} = {}) {
  const wallet = useWalletConnection();
  const label = wallet.pending
    ? "Connecting wallet"
    : wallet.connected && wallet.address
      ? shortAddress(wallet.address)
      : wallet.available
        ? "Connect wallet"
        : "Wallet unavailable";
  const text =
    compact && wallet.pending
      ? "Connecting"
      : compact && !wallet.connected && wallet.available
        ? "Connect"
        : label;
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
      <Wallet size={17} aria-hidden="true" /> <span>{text}</span>
    </Button>
  );
}
