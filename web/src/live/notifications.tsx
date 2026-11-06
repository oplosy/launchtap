"use client";

import Link from "next/link";
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useId,
  useMemo,
  useRef,
  useState,
  useSyncExternalStore,
  type PropsWithChildren,
} from "react";
import type { LiveEvent } from "@/api/types";
import { BellSimple, X } from "@/components/icons";
import { publicConfiguration } from "@/config/public";
import { parseTokenAddress } from "@/token/address";
import { useLiveEvents } from "./live-events";
import { isEventInScope } from "./live-strip-model";
import {
  bellVisible,
  createRuleState,
  evaluate,
  identityChanged,
  type Notification,
  type RuleState,
} from "./notification-rules";
import {
  createNotificationStore,
  safeSessionStorage,
  type NotificationSnapshot,
} from "./notification-store";

/** Who the notifications are for. Supplied from inside the wallet stack when it is mounted. */
export type NotificationIdentity = {
  signedIn: boolean;
  /** Lowercase watched token addresses. */
  watched: readonly string[];
  wallet?: string;
};

export const NO_IDENTITY: NotificationIdentity = { signedIn: false, watched: [] };

const TOAST_VISIBLE_MS = 6_000;
const EMPTY_SNAPSHOT: NotificationSnapshot = { items: [], unread: 0, toast: null };

type NotificationsContextValue = {
  store: ReturnType<typeof createNotificationStore>;
  identity: NotificationIdentity;
  setIdentity: (identity: NotificationIdentity) => void;
};

const NotificationsContext = createContext<NotificationsContextValue | null>(null);

const identityKey = (identity: NotificationIdentity) =>
  JSON.stringify([identity.signedIn, identity.watched, identity.wallet?.toLowerCase() ?? null]);

/**
 * Session notifications derived from the shared live stream. The list lives here, above route
 * changes, so it survives navigation; identity (watched tokens, wallet) is pushed in by a bridge
 * rendered inside the wallet stack, which this provider must not import.
 */
export function NotificationsProvider({ children }: PropsWithChildren) {
  const [store] = useState(() => createNotificationStore({ storage: safeSessionStorage() }));
  const [identity, setIdentityState] = useState<NotificationIdentity>(NO_IDENTITY);
  const identityRef = useRef(identity);
  const ruleState = useRef<RuleState>(createRuleState());

  const setIdentity = useCallback(
    (next: NotificationIdentity) => {
      if (identityKey(next) === identityKey(identityRef.current)) return;
      // Notifications and pending own trades belong to the identity that earned them.
      if (identityChanged(identityRef.current, next)) {
        ruleState.current = createRuleState();
        store.reset();
      }
      identityRef.current = next;
      setIdentityState(next);
    },
    [store],
  );

  const onEvent = useCallback(
    (event: LiveEvent) => {
      if (event.event === "reconnected") return;
      const configuration = publicConfiguration();
      if (configuration.status !== "ready") return;
      if (configuration.chainId === null || configuration.deploymentId === null) return;
      const scope = { chainId: configuration.chainId, deploymentId: configuration.deploymentId };
      if (!isEventInScope(event, scope)) return;
      const current = identityRef.current;
      if (!bellVisible({ signedIn: current.signedIn, connected: Boolean(current.wallet) })) return;
      const notifications = evaluate(
        event,
        {
          watched: current.signedIn ? new Set(current.watched) : new Set(),
          wallet: current.wallet,
        },
        ruleState.current,
      );
      if (notifications.length > 0) store.add(notifications);
    },
    [store],
  );
  useLiveEvents(onEvent);

  const value = useMemo(() => ({ store, identity, setIdentity }), [store, identity, setIdentity]);
  return (
    <NotificationsContext.Provider value={value}>
      {children}
      <ToastRegion />
    </NotificationsContext.Provider>
  );
}

/** Used by the wallet-stack bridge. Inert outside the provider. */
export function useNotificationIdentity() {
  return useContext(NotificationsContext)?.setIdentity ?? null;
}

function useSnapshot() {
  const context = useContext(NotificationsContext);
  const subscribe = useCallback(
    (listener: () => void) => context?.store.subscribe(listener) ?? (() => undefined),
    [context],
  );
  const snapshot = useSyncExternalStore(
    subscribe,
    () => context?.store.getSnapshot() ?? EMPTY_SNAPSHOT,
    () => EMPTY_SNAPSHOT,
  );
  return { context, snapshot };
}

function describe(notification: Notification): string {
  const ticker = notification.symbol.trim() || "Token";
  switch (notification.kind) {
    case "graduation":
      return `${ticker} graduated`;
    case "large_trade":
      return `Large trade on ${ticker}`;
    case "own_trade_safe":
      return `Your ${ticker} trade is safe`;
  }
}

function tokenHref(notification: Notification): string | null {
  const address = parseTokenAddress(notification.token);
  return address ? `/token/${address}` : null;
}

function ToastRegion() {
  const { context, snapshot } = useSnapshot();
  const toast = snapshot.toast;
  const visible = context
    ? bellVisible({
        signedIn: context.identity.signedIn,
        connected: Boolean(context.identity.wallet),
      })
    : false;
  const store = context?.store;
  const toastId = toast?.id;
  useEffect(() => {
    if (!store || toastId === undefined) return;
    const timer = setTimeout(() => store.dismissToast(), TOAST_VISIBLE_MS);
    return () => clearTimeout(timer);
  }, [store, toastId]);
  if (!visible) return null;
  const href = toast && toast.count === 1 ? tokenHref(toast.first) : null;
  const text = toast
    ? toast.count === 1
      ? describe(toast.first)
      : `${toast.count} new notifications`
    : null;
  return (
    <div className="notify-toasts" role="status" aria-live="polite">
      {toast && text ? (
        <div className="notify-toast" key={toast.id}>
          {href ? (
            <Link href={href as never} className="notify-toast-text">
              {text}
            </Link>
          ) : (
            <span className="notify-toast-text">{text}</span>
          )}
          <button
            type="button"
            className="ui-icon-button notify-toast-close"
            aria-label="Dismiss notification"
            onClick={() => store?.dismissToast()}
          >
            <X size={14} aria-hidden="true" />
          </button>
        </div>
      ) : null}
    </div>
  );
}

/** Navbar bell with this session's notifications. Hidden for visitors. */
export function NotificationBell() {
  const { context, snapshot } = useSnapshot();
  const [open, setOpen] = useState(false);
  const panelId = useId();
  const rootRef = useRef<HTMLDivElement>(null);
  const buttonRef = useRef<HTMLButtonElement>(null);
  const store = context?.store;

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

  // While the menu is open everything in it counts as read, including items that arrive later.
  const newestId = snapshot.items[0]?.id;
  useEffect(() => {
    if (open) store?.markAllRead();
  }, [open, newestId, store]);

  if (
    !context ||
    !bellVisible({
      signedIn: context.identity.signedIn,
      connected: Boolean(context.identity.wallet),
    })
  )
    return null;
  const unread = snapshot.unread;
  return (
    <div
      className="notify-bell"
      ref={rootRef}
      onBlur={(event) => {
        const next = event.relatedTarget;
        if (open && next instanceof Node && !event.currentTarget.contains(next)) setOpen(false);
      }}
    >
      <button
        ref={buttonRef}
        type="button"
        className="ui-icon-button notify-bell-button"
        aria-label={unread > 0 ? `Notifications, ${unread} unread` : "Notifications"}
        aria-expanded={open}
        aria-controls={panelId}
        onClick={() => setOpen((value) => !value)}
      >
        <BellSimple size={18} aria-hidden="true" />
        {unread > 0 ? (
          <span className="notify-bell-badge" aria-hidden="true">
            {unread > 9 ? "9+" : unread}
          </span>
        ) : null}
      </button>
      {open ? (
        <section className="notify-panel" id={panelId} aria-label="Notifications">
          {snapshot.items.length === 0 ? (
            <p className="notify-empty">No notifications this session.</p>
          ) : (
            <ul className="notify-list">
              {snapshot.items.map((item) => {
                const href = tokenHref(item);
                return (
                  <li key={item.id} className="notify-item">
                    {href ? (
                      <Link href={href as never} onClick={() => setOpen(false)}>
                        {describe(item)}
                      </Link>
                    ) : (
                      <span>{describe(item)}</span>
                    )}
                    <time className="notify-time mono" dateTime={new Date(item.at).toISOString()}>
                      {new Date(item.at).toLocaleTimeString()}
                    </time>
                  </li>
                );
              })}
            </ul>
          )}
        </section>
      ) : null}
    </div>
  );
}
