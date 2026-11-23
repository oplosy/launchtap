"use client";

import Link from "next/link";
import { useEffect, useId, useRef, useState } from "react";
import { Wallet } from "@/components/icons";
import { usePresence } from "@/components/use-presence";
import { shortAddress } from "@/token/address";
import { accountLinks } from "./account-links";
import { WalletConnectButton, useWalletConnection } from "./connection-context";

/**
 * The navbar's wallet control. Disconnected it is the connect button; connected, the address
 * opens a menu of the account pages plus the wallet actions. Like the notification bell it closes
 * on Escape, on a click outside, and when focus leaves it.
 */
export function AccountMenu() {
  const wallet = useWalletConnection();
  const [open, setOpen] = useState(false);
  const panelId = useId();
  const rootRef = useRef<HTMLDivElement>(null);
  const buttonRef = useRef<HTMLButtonElement>(null);
  const panelMounted = usePresence(open);
  const address = wallet.connected ? wallet.address : undefined;

  useEffect(() => {
    if (!open) return;
    const onKey = (event: KeyboardEvent) => {
      if (event.key !== "Escape") return;
      setOpen(false);
      buttonRef.current?.focus();
    };
    const onPointer = (event: PointerEvent) => {
      if (event.target instanceof Node && !rootRef.current?.contains(event.target)) setOpen(false);
    };
    document.addEventListener("keydown", onKey);
    document.addEventListener("pointerdown", onPointer);
    return () => {
      document.removeEventListener("keydown", onKey);
      document.removeEventListener("pointerdown", onPointer);
    };
  }, [open]);

  if (!address) return <WalletConnectButton variant="primary" compact />;
  const label = shortAddress(address);
  const close = () => setOpen(false);
  return (
    <div
      className="account-menu"
      ref={rootRef}
      onBlur={(event) => {
        const next = event.relatedTarget;
        if (open && next instanceof Node && !event.currentTarget.contains(next)) setOpen(false);
      }}
    >
      <button
        ref={buttonRef}
        type="button"
        className="ui-button ui-button-primary ui-button-sm navbar-wallet"
        aria-label={`Account ${label}`}
        aria-expanded={open}
        aria-controls={panelId}
        onClick={() => setOpen((value) => !value)}
      >
        <span>
          <Wallet size={17} aria-hidden="true" /> <span>{label}</span>
        </span>
      </button>
      {panelMounted ? (
        <nav
          className="account-panel"
          id={panelId}
          aria-label="Account"
          data-state={open ? "open" : "closed"}
          inert={!open}
        >
          <ul className="account-links">
            {accountLinks(address).map(({ href, label: text }) => (
              <li key={href}>
                <Link href={href as never} onClick={close}>
                  {text}
                </Link>
              </li>
            ))}
          </ul>
          <div className="account-actions">
            <button
              type="button"
              onClick={() => {
                close();
                wallet.open();
              }}
            >
              Change wallet
            </button>
            <button
              type="button"
              onClick={() => {
                close();
                wallet.disconnect();
              }}
            >
              Disconnect
            </button>
          </div>
        </nav>
      ) : null}
    </div>
  );
}
