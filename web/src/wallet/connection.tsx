"use client";

import { useConnectOrCreateWallet, usePrivy } from "@privy-io/react-auth";
import { useEffect, useMemo, useRef, useState, type PropsWithChildren } from "react";
import { useAccount, useConnect, useDisconnect } from "wagmi";
import { Wallet } from "@/components/icons";
import { Button, Dialog } from "@/components/primitives";
import { shortAddress } from "@/token/address";
import { WalletConnectionContext, type WalletConnection } from "./connection-context";

export { useWalletConnection, WalletConnectButton } from "./connection-context";

/** Props shared by both bridges; openOnMount opens the picker once the stack has loaded. */
type BridgeProps = PropsWithChildren<{ openOnMount?: boolean }>;

export function WagmiWalletConnectionBridge({ children, openOnMount = false }: BridgeProps) {
  const { connect, connectors, error, isPending } = useConnect();
  const { disconnect } = useDisconnect();
  const { address, isConnected } = useAccount();
  const [open, setOpen] = useState(openOnMount);
  const value = useMemo<WalletConnection>(
    () => ({
      open: () => setOpen(true),
      disconnect: () => disconnect(),
      connected: isConnected,
      pending: isPending,
      available: connectors.length > 0,
      address,
    }),
    [address, connectors.length, disconnect, isConnected, isPending],
  );

  return (
    <WalletConnectionContext.Provider value={value}>
      {children}
      <Dialog
        open={open}
        title={isConnected ? "Wallet connection" : "Choose a wallet"}
        description="Select the wallet that will submit and sign your transactions."
        onClose={() => setOpen(false)}
      >
        <div className="wallet-picker">
          {isConnected && address ? (
            <div className="wallet-picker-current">
              <span>Connected wallet</span>
              <strong className="mono">{shortAddress(address)}</strong>
            </div>
          ) : null}
          <div className="wallet-picker-list" aria-label="Available wallets">
            {connectors.map((connector) => (
              <Button
                key={connector.uid}
                variant="secondary"
                loading={isPending}
                onClick={() =>
                  connect(
                    { connector },
                    {
                      onSuccess: () => setOpen(false),
                    },
                  )
                }
              >
                <Wallet size={18} aria-hidden="true" />
                {walletConnectorLabel(connector.name)}
              </Button>
            ))}
          </div>
          {connectors.length === 0 ? (
            <p className="wallet-picker-error" role="status">
              No compatible browser wallet was detected.
            </p>
          ) : null}
          {error ? (
            <p className="wallet-picker-error" role="alert">
              The wallet did not connect. Choose a wallet and try again.
            </p>
          ) : null}
          {isConnected ? (
            <Button
              variant="quiet"
              onClick={() => {
                disconnect();
                setOpen(false);
              }}
            >
              Disconnect
            </Button>
          ) : null}
        </div>
      </Dialog>
    </WalletConnectionContext.Provider>
  );
}

export function PrivyWalletConnectionBridge({ children, openOnMount = false }: BridgeProps) {
  // The launch flow requires a Privy-authenticated user with the selected wallet
  // in linkedAccounts. connectWallet() only connects an external wallet and can
  // leave the app in connected-unlinked state, which correctly blocks signing.
  // connectOrCreateWallet() completes the wallet auth/link flow as one operation.
  const { connectOrCreateWallet } = useConnectOrCreateWallet();
  const { ready } = usePrivy();
  const { disconnect } = useDisconnect();
  const { address, isConnected } = useAccount();
  const openedOnMount = useRef(false);
  useEffect(() => {
    if (!openOnMount || !ready || openedOnMount.current) return;
    openedOnMount.current = true;
    if (!isConnected) connectOrCreateWallet();
  }, [connectOrCreateWallet, isConnected, openOnMount, ready]);
  const value = useMemo<WalletConnection>(
    () => ({
      open: () => connectOrCreateWallet(),
      disconnect: () => disconnect(),
      connected: isConnected,
      pending: false,
      available: true,
      address,
    }),
    [address, connectOrCreateWallet, disconnect, isConnected],
  );
  return (
    <WalletConnectionContext.Provider value={value}>{children}</WalletConnectionContext.Provider>
  );
}

export function walletConnectorLabel(name: string) {
  const label = name.trim();
  if (!label || /^injected$/i.test(label)) return "Browser wallet";
  return label;
}
