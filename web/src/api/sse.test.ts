import { afterEach, describe, expect, it, vi } from "vitest";
import { SseInvalidationStream, type EventSourceLike } from "./sse";

afterEach(() => vi.useRealTimers());

function sourceHarness(order: string[]) {
  let currentError: (() => void) | undefined;
  const listeners = new Map<string, (event: Event) => void>();
  const sources: EventSourceLike[] = [];
  const factory = () => {
    order.push("connect");
    const source: EventSourceLike = {
      addEventListener: (name, listener) => {
        if (name === "error") currentError = () => listener(new Event("error"));
        else listeners.set(name, listener);
      },
      close: () => order.push("close"),
    };
    sources.push(source);
    return source;
  };
  return {
    factory,
    sources,
    triggerError: () => currentError?.(),
    open: () => listeners.get("open")?.(new Event("open")),
    emit: (name: string, data: unknown) =>
      listeners.get(name)?.(new MessageEvent(name, { data: JSON.stringify(data) })),
  };
}

describe("SSE invalidation", () => {
  it("refetches REST before reconnecting", async () => {
    vi.useFakeTimers();
    const order: string[] = [];
    const harness = sourceHarness(order);
    const stream = new SseInvalidationStream({
      url: "https://api.example/events",
      eventSourceFactory: harness.factory,
      queryClient: { invalidateQueries: async () => undefined },
      refetchSnapshot: async () => {
        order.push("rest");
      },
    });
    stream.start();
    harness.triggerError();
    await vi.advanceTimersByTimeAsync(250);
    expect(order.indexOf("rest")).toBeLessThan(order.lastIndexOf("connect"));
    stream.stop();
    expect(harness.sources.length).toBe(2);
  });

  it("completes token, candles, and active collection refreshes before reconnecting", async () => {
    vi.useFakeTimers();
    const order: string[] = [];
    const harness = sourceHarness(order);
    const stream = new SseInvalidationStream({
      url: "https://api.example/events",
      eventSourceFactory: harness.factory,
      queryClient: { invalidateQueries: async () => undefined },
      refetchSnapshot: async () => {
        order.push("token");
        await Promise.resolve();
        order.push("candles");
        await Promise.resolve();
        order.push("collection");
      },
    });
    stream.start();
    harness.triggerError();
    await vi.advanceTimersByTimeAsync(250);
    expect(order.slice(0, 4)).toEqual(["connect", "close", "token", "candles"]);
    await Promise.resolve();
    expect(order).toEqual(["connect", "close", "token", "candles", "collection", "connect"]);
    stream.stop();
  });

  it("retries REST after transient failure and reconnects only after success", async () => {
    vi.useFakeTimers();
    const order: string[] = [];
    const harness = sourceHarness(order);
    let attempts = 0;
    const stream = new SseInvalidationStream({
      url: "https://api.example/events",
      eventSourceFactory: harness.factory,
      queryClient: { invalidateQueries: async () => undefined },
      retry: { baseDelayMs: 100, maxAttempts: 3 },
      refetchSnapshot: async () => {
        attempts += 1;
        order.push(`rest-${attempts}`);
        if (attempts === 1) throw new Error("temporary");
      },
    });
    stream.start();
    harness.triggerError();
    await vi.advanceTimersByTimeAsync(100);
    expect(harness.sources.length).toBe(1);
    await vi.advanceTimersByTimeAsync(200);
    expect(harness.sources.length).toBe(2);
    expect(order.indexOf("rest-2")).toBeLessThan(order.lastIndexOf("connect"));
    stream.stop();
  });

  it("backs off and caps reconnects when REST succeeds but every SSE source errors", async () => {
    vi.useFakeTimers();
    const order: string[] = [];
    const harness = sourceHarness(order);
    let attempts = 0;
    let exhausted = 0;
    const stream = new SseInvalidationStream({
      url: "https://api.example/events",
      eventSourceFactory: harness.factory,
      queryClient: { invalidateQueries: async () => undefined },
      retry: { baseDelayMs: 100, maxDelayMs: 1_000, maxAttempts: 3 },
      onRecoveryExhausted: () => {
        exhausted += 1;
      },
      refetchSnapshot: async () => {
        attempts += 1;
      },
    });
    stream.start();
    harness.triggerError();
    await vi.advanceTimersByTimeAsync(100);
    expect(attempts).toBe(1);
    expect(harness.sources).toHaveLength(2);

    harness.triggerError();
    await vi.advanceTimersByTimeAsync(199);
    expect(attempts).toBe(1);
    await vi.advanceTimersByTimeAsync(1);
    expect(attempts).toBe(2);
    expect(harness.sources).toHaveLength(3);

    harness.triggerError();
    await vi.advanceTimersByTimeAsync(10_000);
    expect(attempts).toBe(2);
    expect(harness.sources).toHaveLength(3);
    expect(exhausted).toBe(1);
    stream.stop();
  });

  it("disposal cancels pending recovery retries", async () => {
    vi.useFakeTimers();
    const order: string[] = [];
    const harness = sourceHarness(order);
    let attempts = 0;
    const stream = new SseInvalidationStream({
      url: "https://api.example/events",
      eventSourceFactory: harness.factory,
      queryClient: { invalidateQueries: async () => undefined },
      retry: { baseDelayMs: 100 },
      refetchSnapshot: async () => {
        attempts += 1;
        throw new Error("offline");
      },
    });
    stream.start();
    harness.triggerError();
    await vi.advanceTimersByTimeAsync(99);
    stream.stop();
    await vi.advanceTimersByTimeAsync(10_000);
    expect(attempts).toBe(0);
    expect(harness.sources.length).toBe(1);
  });

  it("aborts an in-flight REST refetch on disposal", async () => {
    vi.useFakeTimers();
    const order: string[] = [];
    const harness = sourceHarness(order);
    let signal: AbortSignal | undefined;
    const stream = new SseInvalidationStream({
      url: "https://api.example/events",
      eventSourceFactory: harness.factory,
      queryClient: { invalidateQueries: async () => undefined },
      refetchSnapshot: (nextSignal) =>
        new Promise<void>((_, reject) => {
          signal = nextSignal;
          nextSignal?.addEventListener("abort", () => reject(new Error("aborted")));
        }),
    });
    stream.start();
    harness.triggerError();
    await vi.advanceTimersByTimeAsync(250);
    stream.stop();
    expect(signal?.aborted).toBe(true);
  });

  it("invalidates relevant events and ignores events from another deployment", async () => {
    const order: string[] = [];
    const harness = sourceHarness(order);
    const invalidated: unknown[] = [];
    const stream = new SseInvalidationStream({
      url: "https://api.example/events",
      eventSourceFactory: harness.factory,
      queryClient: {
        invalidateQueries: async (value) => {
          invalidated.push(value);
        },
      },
      queryKeyForEvent: (event) =>
        event.data.chain_id === 1 && event.data.deployment_id === "testnet"
          ? ["tokens", "snapshot"]
          : undefined,
      refetchSnapshot: async () => undefined,
    });
    stream.start();
    harness.emit("token", { chain_id: 1, deployment_id: "testnet", token: "0x1" });
    harness.emit("token", { chain_id: 2, deployment_id: "other", token: "0x2" });
    await Promise.resolve();
    expect(invalidated).toHaveLength(1);
    stream.stop();
  });

  it("dispatches trade, graduation, new_token, trade_burst and finality to listeners", () => {
    const harness = sourceHarness([]);
    const seen: string[] = [];
    const invalidated: unknown[] = [];
    const stream = new SseInvalidationStream({
      url: "https://api.example/events",
      eventSourceFactory: harness.factory,
      queryClient: { invalidateQueries: async (v) => void invalidated.push(v) },
      queryKeyForEvent: () => ["k"],
      onEvent: (event) => seen.push(event.event),
      refetchSnapshot: async () => undefined,
    });
    stream.start();
    const base = { chain_id: 1, deployment_id: "local" };
    for (const name of ["trade", "graduation", "new_token", "trade_burst", "finality"]) {
      harness.emit(name, { ...base });
    }
    expect(seen).toEqual(["trade", "graduation", "new_token", "trade_burst", "finality"]);
    expect(invalidated).toHaveLength(0);
    stream.stop();
  });

  it("opens a single EventSource for multiple subscribers", () => {
    const harness = sourceHarness([]);
    const a: string[] = [];
    const b: string[] = [];
    const subscribers = new Set<(event: { event: string }) => void>();
    subscribers.add((e) => a.push(e.event));
    subscribers.add((e) => b.push(e.event));
    const stream = new SseInvalidationStream({
      url: "https://api.example/events",
      eventSourceFactory: harness.factory,
      queryClient: { invalidateQueries: async () => undefined },
      onEvent: (event) => subscribers.forEach((s) => s(event)),
      refetchSnapshot: async () => undefined,
    });
    stream.start();
    harness.emit("trade", { chain_id: 1, deployment_id: "local" });
    expect(harness.sources).toHaveLength(1);
    expect(a).toEqual(["trade"]);
    expect(b).toEqual(["trade"]);
    stream.stop();
  });

  it("calls onReconnected after REST recovery reopens the source", async () => {
    vi.useFakeTimers();
    const order: string[] = [];
    const harness = sourceHarness(order);
    const stream = new SseInvalidationStream({
      url: "https://api.example/events",
      eventSourceFactory: harness.factory,
      queryClient: { invalidateQueries: async () => undefined },
      refetchSnapshot: async () => void order.push("rest"),
      onReconnected: () => order.push("reconnected"),
    });
    stream.start();
    harness.triggerError();
    await vi.advanceTimersByTimeAsync(250);
    expect(order).toEqual(["connect", "close", "rest", "connect"]);
    harness.open();
    expect(order).toEqual(["connect", "close", "rest", "connect", "reconnected"]);
    stream.stop();
  });

  it("emits reconnected on the reopened source's open event, once, never on the first open", async () => {
    vi.useFakeTimers();
    const order: string[] = [];
    const harness = sourceHarness(order);
    let reconnected = 0;
    const stream = new SseInvalidationStream({
      url: "https://api.example/events",
      eventSourceFactory: harness.factory,
      queryClient: { invalidateQueries: async () => undefined },
      refetchSnapshot: async () => undefined,
      onReconnected: () => {
        reconnected += 1;
      },
    });
    stream.start();
    harness.open();
    expect(reconnected).toBe(0);
    harness.triggerError();
    await vi.advanceTimersByTimeAsync(250);
    expect(harness.sources).toHaveLength(2);
    expect(reconnected).toBe(0);
    harness.open();
    harness.open();
    expect(reconnected).toBe(1);
    stream.stop();
  });

  it("does not emit reconnected when the reopened source fails before opening", async () => {
    vi.useFakeTimers();
    const order: string[] = [];
    const harness = sourceHarness(order);
    let reconnected = 0;
    const stream = new SseInvalidationStream({
      url: "https://api.example/events",
      eventSourceFactory: harness.factory,
      queryClient: { invalidateQueries: async () => undefined },
      refetchSnapshot: async () => undefined,
      onReconnected: () => {
        reconnected += 1;
      },
    });
    stream.start();
    harness.triggerError();
    await vi.advanceTimersByTimeAsync(250);
    harness.triggerError();
    expect(reconnected).toBe(0);
    await vi.advanceTimersByTimeAsync(500);
    expect(harness.sources).toHaveLength(3);
    harness.open();
    expect(reconnected).toBe(1);
    stream.stop();
  });

  it("start({ reconnected: true }) announces the first open as a reconnection", () => {
    const order: string[] = [];
    const harness = sourceHarness(order);
    let reconnected = 0;
    const stream = new SseInvalidationStream({
      url: "https://api.example/events",
      eventSourceFactory: harness.factory,
      queryClient: { invalidateQueries: async () => undefined },
      refetchSnapshot: async () => undefined,
      onReconnected: () => {
        reconnected += 1;
      },
    });
    stream.start({ reconnected: true });
    expect(reconnected).toBe(0);
    harness.open();
    expect(reconnected).toBe(1);
    stream.stop();
  });
});
