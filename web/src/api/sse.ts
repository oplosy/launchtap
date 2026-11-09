import type { QueryClient, QueryKey } from "@tanstack/react-query";
import type { ApiEvent, ApiEventName } from "./types";

export type EventSourceLike = {
  addEventListener: (type: string, listener: (event: Event) => void) => void;
  close: () => void;
};
export type EventSourceFactory = (url: string) => EventSourceLike;

/** SSE is a best-effort invalidation hint. Canonical data always comes from REST. */
export class SseInvalidationStream {
  private source: EventSourceLike | null = null;
  private reconnecting = false;
  private stopped = false;
  private retryTimer: ReturnType<typeof setTimeout> | null = null;
  private stableConnectionTimer: ReturnType<typeof setTimeout> | null = null;
  private retryAttempt = 0;
  private recoveryAbortController: AbortController | null = null;
  /** Set when the next source's `open` event completes a reconnection (fires onReconnected). */
  private announceOnOpen = false;
  constructor(
    private readonly options: {
      url: string;
      queryClient: Pick<QueryClient, "invalidateQueries">;
      refetchSnapshot: (signal?: AbortSignal) => Promise<void>;
      eventSourceFactory?: EventSourceFactory;
      queryKeyForEvent?: (event: ApiEvent) => QueryKey | undefined;
      retry?: { maxAttempts?: number; baseDelayMs?: number; maxDelayMs?: number };
      onRecoveryExhausted?: () => void;
      /** Receives every validated event (all event types), before query invalidation hints. */
      onEvent?: (event: ApiEvent) => void;
      /**
       * Called once after REST was refetched and the fresh EventSource actually opened (its `open`
       * event), so a refetch triggered by it cannot race events the new source has not delivered.
       */
      onReconnected?: () => void;
    },
  ) {}

  /** `reconnected: true` announces the first open as a reconnection (a restarted stream). */
  start(options: { reconnected?: boolean } = {}) {
    this.stopped = false;
    this.announceOnOpen = options.reconnected === true;
    this.connect();
    return () => this.stop();
  }

  stop() {
    this.stopped = true;
    this.recoveryAbortController?.abort();
    this.recoveryAbortController = null;
    if (this.retryTimer) clearTimeout(this.retryTimer);
    this.retryTimer = null;
    if (this.stableConnectionTimer) clearTimeout(this.stableConnectionTimer);
    this.stableConnectionTimer = null;
    this.source?.close();
    this.source = null;
    this.announceOnOpen = false;
  }

  private connect() {
    if (this.stopped) return;
    const factory = this.options.eventSourceFactory ?? ((url) => new EventSource(url));
    const source = factory(this.options.url);
    this.source = source;
    source.addEventListener("open", () => {
      if (this.source !== source) return;
      if (this.stableConnectionTimer) clearTimeout(this.stableConnectionTimer);
      this.stableConnectionTimer = setTimeout(() => {
        this.stableConnectionTimer = null;
        if (this.source === source) this.retryAttempt = 0;
      }, 30_000);
      if (this.announceOnOpen) {
        this.announceOnOpen = false;
        this.options.onReconnected?.();
      }
    });
    for (const eventType of API_EVENT_NAMES) {
      source.addEventListener(eventType, (event) => {
        if (this.source === source) this.handleEvent(eventType, event as MessageEvent<string>);
      });
    }
    source.addEventListener("error", () => {
      if (this.source !== source) return;
      void this.reconnect();
    });
  }

  private handleEvent(eventType: string, event: MessageEvent<string>) {
    let data: unknown;
    try {
      data = JSON.parse(event.data) as unknown;
    } catch {
      return;
    }
    if (!isApiEvent(eventType, data)) return;
    this.retryAttempt = 0;
    if (this.stableConnectionTimer) clearTimeout(this.stableConnectionTimer);
    this.stableConnectionTimer = null;
    const apiEvent = { event: eventType, data } as ApiEvent;
    this.options.onEvent?.(apiEvent);
    // Invalidation hints keep their original scope: only launch, token and reorg events.
    if (!INVALIDATION_EVENT_NAMES.has(eventType as ApiEventName)) return;
    const queryKey = this.options.queryKeyForEvent?.(apiEvent);
    if (queryKey) void this.options.queryClient.invalidateQueries({ queryKey });
  }

  private async reconnect() {
    if (this.reconnecting || this.stopped) return;
    this.reconnecting = true;
    if (this.stableConnectionTimer) clearTimeout(this.stableConnectionTimer);
    this.stableConnectionTimer = null;
    this.source?.close();
    this.source = null;
    this.retryAttempt += 1;
    this.scheduleRecovery();
    this.reconnecting = false;
  }

  private scheduleRecovery() {
    if (this.stopped || this.retryTimer) return;
    const retry = this.options.retry ?? {};
    const maxAttempts = retry.maxAttempts ?? 5;
    if (this.retryAttempt >= maxAttempts) {
      this.options.onRecoveryExhausted?.();
      return;
    }
    const baseDelay = retry.baseDelayMs ?? 250;
    const maxDelay = retry.maxDelayMs ?? 5_000;
    const delay = Math.min(maxDelay, baseDelay * 2 ** Math.max(0, this.retryAttempt - 1));
    this.retryTimer = setTimeout(() => {
      this.retryTimer = null;
      void this.performRecovery();
    }, delay);
  }

  private async performRecovery() {
    if (this.stopped) return;
    const abortController = new AbortController();
    this.recoveryAbortController = abortController;
    try {
      // Ordering is intentional: refill canonical REST state before accepting new hints.
      await this.options.refetchSnapshot(abortController.signal);
      if (!this.stopped) {
        // onReconnected fires on this source's `open` event, not here: the connection may still
        // fail, and a refetch before it opens could miss events published in between.
        this.announceOnOpen = true;
        this.connect();
      }
    } catch {
      this.retryAttempt += 1;
      this.scheduleRecovery();
    } finally {
      if (this.recoveryAbortController === abortController) this.recoveryAbortController = null;
    }
  }
}

const API_EVENT_NAMES: readonly ApiEventName[] = [
  "launch",
  "token",
  "reorg",
  "trade",
  "graduation",
  "new_token",
  "trade_burst",
  "finality",
];
const INVALIDATION_EVENT_NAMES: ReadonlySet<ApiEventName> = new Set(["launch", "token", "reorg"]);

function isApiEvent(event: string, data: unknown): data is ApiEvent["data"] {
  if (!(API_EVENT_NAMES as readonly string[]).includes(event)) return false;
  return (
    typeof data === "object" &&
    data !== null &&
    typeof (data as { chain_id?: unknown }).chain_id === "number" &&
    typeof (data as { deployment_id?: unknown }).deployment_id === "string"
  );
}
