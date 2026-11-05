import { describe, expect, it, vi } from "vitest";
import { ApiProblem } from "@/api/problems";
import {
  WATCHLIST_LIMIT,
  createWatchlistStore,
  type WatchlistApi,
  type WatchlistAuth,
} from "./use-watchlist";

const TOKEN = "0x00000000000000000000000000000000000000aa";
const OTHER = "0x00000000000000000000000000000000000000bb";

function setup(overrides: Partial<WatchlistApi> = {}, auth: Partial<WatchlistAuth> = {}) {
  const api: WatchlistApi = {
    list: vi.fn(async () => ({ items: [] })),
    add: vi.fn(async () => undefined),
    remove: vi.fn(async () => undefined),
    ...overrides,
  };
  const login = vi.fn();
  const deps = {
    api,
    auth: {
      available: true,
      ready: true,
      authenticated: true,
      getAuth: async () => ({ accessToken: "a", identityToken: "i" }),
      login,
      ...auth,
    } satisfies WatchlistAuth,
  };
  return { store: createWatchlistStore(), api, login, deps };
}

describe("watchlist store", () => {
  it("adds optimistically and keeps the entry on success", async () => {
    let release: () => void = () => undefined;
    const { store, api, deps } = setup({
      add: vi.fn(() => new Promise<void>((resolve) => (release = resolve))),
    });
    const pending = store.toggle(TOKEN, deps);
    await Promise.resolve();
    await Promise.resolve();
    expect(store.getSnapshot().addresses.has(TOKEN)).toBe(true);
    release();
    await pending;
    expect(api.add).toHaveBeenCalledWith(TOKEN, { accessToken: "a", identityToken: "i" });
    expect(store.getSnapshot().addresses.has(TOKEN)).toBe(true);
    expect(store.getSnapshot().toast).toBeNull();
  });

  it("rolls back and shows an error toast when the server fails", async () => {
    const { store, deps } = setup({
      add: vi.fn(async () => {
        throw new ApiProblem(500, "service_unavailable");
      }),
    });
    await store.toggle(TOKEN, deps);
    expect(store.getSnapshot().addresses.has(TOKEN)).toBe(false);
    expect(store.getSnapshot().toast).toMatchObject({ tone: "danger" });
  });

  it("rolls back on 409 and reports the full watchlist", async () => {
    const { store, deps } = setup({
      add: vi.fn(async () => {
        throw new ApiProblem(409, "watchlist_full");
      }),
    });
    await store.toggle(TOKEN, deps);
    const snapshot = store.getSnapshot();
    expect(snapshot.addresses.has(TOKEN)).toBe(false);
    expect(snapshot.toast?.title).toBe("Watchlist full (200)");
    expect(snapshot.full).toBe(true);
  });

  it("does not send a request once the local list is already full", async () => {
    const items = Array.from({ length: WATCHLIST_LIMIT }, (_, index) => ({
      address: `0x${(index + 4096).toString(16).padStart(40, "0")}`,
      added_at: "2026-11-05T00:00:00Z",
      token: null,
    }));
    const { store, api, deps } = setup({ list: vi.fn(async () => ({ items })) });
    await store.load(deps);
    await store.toggle(OTHER, deps);
    expect(api.add).not.toHaveBeenCalled();
    expect(store.getSnapshot().toast?.title).toBe("Watchlist full (200)");
  });

  it("removes optimistically and restores the entry on failure", async () => {
    const { store, deps } = setup({
      list: vi.fn(async () => ({
        items: [{ address: TOKEN, added_at: "2026-11-05T00:00:00Z", token: null }],
      })),
      remove: vi.fn(async () => {
        throw new ApiProblem(500, "service_unavailable");
      }),
    });
    await store.load(deps);
    expect(store.getSnapshot().addresses.has(TOKEN)).toBe(true);
    await store.toggle(TOKEN, deps);
    expect(store.getSnapshot().addresses.has(TOKEN)).toBe(true);
    expect(store.getSnapshot().toast).toMatchObject({ tone: "danger" });
  });

  it("signs in instead of sending a request when signed out", async () => {
    const { store, api, login, deps } = setup({}, { authenticated: false });
    await store.toggle(TOKEN, deps);
    expect(login).toHaveBeenCalledTimes(1);
    expect(api.add).not.toHaveBeenCalled();
    expect(store.getSnapshot().addresses.has(TOKEN)).toBe(false);
  });

  it("signs in again and rolls back on 401", async () => {
    const { store, login, deps } = setup({
      add: vi.fn(async () => {
        throw new ApiProblem(401, "authentication_required");
      }),
    });
    await store.toggle(TOKEN, deps);
    expect(login).toHaveBeenCalledTimes(1);
    expect(store.getSnapshot().addresses.has(TOKEN)).toBe(false);
  });

  it("matches addresses case-insensitively", async () => {
    const { store, deps } = setup();
    await store.toggle(TOKEN.toUpperCase().replace("0X", "0x"), deps);
    expect(store.getSnapshot().addresses.has(TOKEN)).toBe(true);
  });

  it("shares a load already in flight instead of restarting it", async () => {
    const { store, api, deps } = setup();
    // The tab and the hook both ask to load on mount; a re-render must not trigger more requests.
    await Promise.all([store.load(deps), store.load(deps), store.load(deps)]);
    expect(api.list).toHaveBeenCalledTimes(1);
    expect(store.getSnapshot().status).toBe("ready");
    await store.load(deps);
    expect(api.list).toHaveBeenCalledTimes(2);
  });

  it("keeps an in-flight optimistic add when a load lands meanwhile", async () => {
    let release: () => void = () => undefined;
    const { store, deps } = setup({
      add: vi.fn(() => new Promise<void>((resolve) => (release = resolve))),
      list: vi.fn(async () => ({ items: [] })),
    });
    const toggling = store.toggle(TOKEN, deps);
    await Promise.resolve();
    await Promise.resolve();
    await store.load(deps);
    expect(store.getSnapshot().addresses.has(TOKEN)).toBe(true);
    release();
    await toggling;
    expect(store.getSnapshot().addresses.has(TOKEN)).toBe(true);
  });

  it("keeps an in-flight optimistic removal when a load lands meanwhile", async () => {
    let release: () => void = () => undefined;
    const item = { address: TOKEN, added_at: "2026-11-05T00:00:00Z", token: null };
    const { store, deps } = setup({
      list: vi.fn(async () => ({ items: [item] })),
      remove: vi.fn(() => new Promise<void>((resolve) => (release = resolve))),
    });
    await store.load(deps);
    const toggling = store.toggle(TOKEN, deps);
    await Promise.resolve();
    await Promise.resolve();
    await store.load(deps);
    expect(store.getSnapshot().addresses.has(TOKEN)).toBe(false);
    release();
    await toggling;
    expect(store.getSnapshot().addresses.has(TOKEN)).toBe(false);
  });
});
