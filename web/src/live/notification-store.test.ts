import { describe, expect, it } from "vitest";
import type { Notification } from "./notification-rules";
import {
  LAST_READ_KEY,
  collapseToast,
  createNotificationStore,
  type KeyValueStorage,
} from "./notification-store";

function note(id: string, at: number): Notification {
  return { id, kind: "graduation", token: "0xa", symbol: "T", txHash: "0x1", at };
}

function memoryStorage(): KeyValueStorage & { data: Map<string, string> } {
  const data = new Map<string, string>();
  return {
    data,
    getItem: (key) => data.get(key) ?? null,
    setItem: (key, value) => void data.set(key, value),
  };
}

describe("collapseToast", () => {
  it("collapses toasts arriving within 5 seconds", () => {
    const first = collapseToast(null, [note("a", 0)], 0, 1);
    const second = collapseToast(first, [note("b", 4000), note("c", 4000)], 4_999, 2);
    expect(second).toMatchObject({ id: 1, count: 3 });
    const later = collapseToast(second, [note("d", 5000)], 5_000, 2);
    expect(later).toMatchObject({ id: 2, count: 1 });
  });
});

describe("notification store", () => {
  it("keeps last-read in storage only and the list in memory", () => {
    const storage = memoryStorage();
    let clock = 1_000;
    const store = createNotificationStore({ storage, now: () => clock });
    store.add([note("a", 1_000)]);
    expect(storage.data.size).toBe(0);
    expect(store.getSnapshot().unread).toBe(1);
    clock = 2_000;
    store.markAllRead();
    expect(store.getSnapshot().unread).toBe(0);
    expect([...storage.data.keys()]).toEqual([LAST_READ_KEY]);
    expect(storage.data.get(LAST_READ_KEY)).toBe("2000");
    // A new store (page reload) sees last-read but not the list.
    const reloaded = createNotificationStore({ storage, now: () => clock });
    expect(reloaded.getSnapshot().items).toEqual([]);
    reloaded.add([note("old", 1_500), note("new", 2_500)]);
    expect(reloaded.getSnapshot().unread).toBe(1);
  });

  it("survives blocked storage", () => {
    const broken: KeyValueStorage = {
      getItem: () => {
        throw new Error("blocked");
      },
      setItem: () => {
        throw new Error("blocked");
      },
    };
    const store = createNotificationStore({ storage: broken, now: () => 1 });
    store.add([note("a", 1)]);
    expect(() => store.markAllRead()).not.toThrow();
    expect(store.getSnapshot().unread).toBe(0);
  });

  it("de-duplicates by id and lists newest first", () => {
    const store = createNotificationStore({ storage: null, now: () => 1 });
    store.add([note("a", 1), note("b", 2)]);
    store.add([note("a", 3)]);
    expect(store.getSnapshot().items.map((n) => n.id)).toEqual(["b", "a"]);
  });
});

describe("notification store reset and de-duplication", () => {
  it("reset clears list, unread badge and toast", () => {
    const store = createNotificationStore({ storage: null, now: () => 10 });
    store.add([note("a", 10)]);
    expect(store.getSnapshot().toast).not.toBeNull();
    store.reset();
    expect(store.getSnapshot()).toMatchObject({ items: [], unread: 0, toast: null });
    store.add([note("a", 11)]);
    expect(store.getSnapshot().items).toHaveLength(1);
  });

  it("drops a repeated id even after the visible list rolled over", () => {
    const store = createNotificationStore({ storage: null, now: () => 1, max: 2 });
    store.add([note("a", 1)]);
    store.add([note("b", 2), note("c", 3)]);
    expect(store.getSnapshot().items.map((n) => n.id)).toEqual(["c", "b"]);
    store.add([note("a", 4)]);
    expect(store.getSnapshot().items.map((n) => n.id)).toEqual(["c", "b"]);
  });

  it("bounds the remembered ids", () => {
    const store = createNotificationStore({ storage: null, now: () => 1, max: 1, seenMax: 2 });
    store.add([note("a", 1)]);
    store.add([note("b", 2)]);
    store.add([note("c", 3)]);
    store.add([note("a", 4)]);
    expect(store.getSnapshot().items.map((n) => n.id)).toEqual(["a"]);
  });
});
