import type { Notification } from "./notification-rules";

export const TOAST_COLLAPSE_MS = 5_000;
export const NOTIFICATION_MAX_ITEMS = 50;
export const LAST_READ_KEY = "launchpad:notifications:last-read";

/** The slice of Web Storage the store needs; injected so it is testable without a DOM. */
export type KeyValueStorage = {
  getItem: (key: string) => string | null;
  setItem: (key: string, value: string) => void;
};

/** `sessionStorage` or null when the browser blocks or lacks it. */
export function safeSessionStorage(): KeyValueStorage | null {
  try {
    return typeof sessionStorage === "undefined" ? null : sessionStorage;
  } catch {
    return null;
  }
}

export function readLastRead(storage: KeyValueStorage | null): number {
  try {
    const value = Number(storage?.getItem(LAST_READ_KEY));
    return Number.isFinite(value) && value > 0 ? value : 0;
  } catch {
    return 0;
  }
}

export function writeLastRead(storage: KeyValueStorage | null, at: number): void {
  try {
    storage?.setItem(LAST_READ_KEY, String(at));
  } catch {
    // Storage may be full or blocked; last-read then lasts only until reload.
  }
}

export type Toast = {
  id: number;
  /** First notification in this toast. */
  first: Notification;
  count: number;
  /** When the toast opened; the collapse window runs from here. */
  at: number;
};

/** Folds arrivals into the visible toast while within the window, else opens a new one. */
export function collapseToast(
  current: Toast | null,
  incoming: readonly Notification[],
  now: number,
  nextId: number,
  windowMs: number = TOAST_COLLAPSE_MS,
): Toast | null {
  const first = incoming[0];
  if (!first) return current;
  if (current && now - current.at < windowMs) {
    return { ...current, count: current.count + incoming.length };
  }
  return { id: nextId, first, count: incoming.length, at: now };
}

export type NotificationSnapshot = {
  items: readonly Notification[];
  unread: number;
  toast: Toast | null;
};

export function createNotificationStore(options: {
  storage: KeyValueStorage | null;
  now?: () => number;
  max?: number;
}) {
  const now = options.now ?? Date.now;
  const max = options.max ?? NOTIFICATION_MAX_ITEMS;
  let items: Notification[] = [];
  let lastRead = readLastRead(options.storage);
  let toast: Toast | null = null;
  let toastId = 0;
  const listeners = new Set<() => void>();
  let snapshot: NotificationSnapshot = { items, unread: 0, toast };

  const publish = () => {
    snapshot = { items, unread: items.filter((item) => item.at > lastRead).length, toast };
    listeners.forEach((listener) => listener());
  };

  return {
    subscribe(listener: () => void) {
      listeners.add(listener);
      return () => void listeners.delete(listener);
    },
    getSnapshot: () => snapshot,
    /** Adds notifications, skipping ids already listed (replays after a reconnect). */
    add(incoming: readonly Notification[]) {
      const seen = new Set(items.map((item) => item.id));
      const fresh = incoming.filter((item) => {
        if (seen.has(item.id)) return false;
        seen.add(item.id);
        return true;
      });
      if (fresh.length === 0) return;
      items = [...fresh.slice().reverse(), ...items].slice(0, max);
      toast = collapseToast(toast, fresh, now(), toastId + 1);
      if (toast && toast.id > toastId) toastId = toast.id;
      publish();
    },
    dismissToast() {
      if (!toast) return;
      toast = null;
      publish();
    },
    markAllRead() {
      const newest = items.reduce((latest, item) => Math.max(latest, item.at), 0);
      lastRead = Math.max(now(), newest);
      writeLastRead(options.storage, lastRead);
      publish();
    },
  };
}

export type NotificationStore = ReturnType<typeof createNotificationStore>;
