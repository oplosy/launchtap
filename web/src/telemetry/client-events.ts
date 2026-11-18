import { publicConfiguration } from "@/config/public";
import { routeGroup, type RouteGroup } from "./route-group";

/**
 * First-party browser telemetry (docs/specs/2026-11-18-client-telemetry-preflight.md). Only
 * closed-set values and one number per vital leave the browser; reporting never throws or logs.
 */
export type ErrorSource =
  "route_boundary" | "global_boundary" | "window_error" | "unhandled_rejection";
export type VitalName = "LCP" | "INP" | "CLS" | "FCP" | "TTFB";

type ClientEvent =
  | { type: "error"; source: ErrorSource; route: RouteGroup; class: string }
  | { type: "vital"; name: VitalName; route: RouteGroup; value: number };

const ERROR_CLASSES = new Set([
  "Error",
  "TypeError",
  "ReferenceError",
  "RangeError",
  "SyntaxError",
  "ChunkLoadError",
]);
const VITALS = new Set<string>(["LCP", "INP", "CLS", "FCP", "TTFB"]);
export const MAX_BATCH = 20;
export const MAX_ERRORS_PER_LOAD = 10;
export const FLUSH_DELAY_MS = 5_000;
const MAX_TIMING_MS = 60_000;
const MAX_CLS = 10;

export function errorClass(error: unknown): string {
  const name = error instanceof Error ? error.name : undefined;
  return name && ERROR_CLASSES.has(name) ? name : "other";
}

export type ReporterOptions = {
  /** Sends one JSON batch; null disables reporting. */
  send: ((body: string) => void) | null;
  schedule?: (flush: () => void, delayMs: number) => void;
};

export function createClientEventReporter({ send, schedule = defaultSchedule }: ReporterOptions) {
  let queue: ClientEvent[] = [];
  let scheduled = false;
  let errors = 0;
  const seenErrors = new Set<string>();
  const seenVitals = new Set<string>();

  function flush() {
    scheduled = false;
    while (send && queue.length > 0) {
      const batch = queue.slice(0, MAX_BATCH);
      queue = queue.slice(MAX_BATCH);
      try {
        send(JSON.stringify({ events: batch }));
      } catch {
        // Telemetry is best effort.
      }
    }
  }

  function enqueue(event: ClientEvent) {
    if (!send) return;
    queue.push(event);
    if (queue.length >= MAX_BATCH) flush();
    else if (!scheduled) {
      scheduled = true;
      schedule(flush, FLUSH_DELAY_MS);
    }
  }

  return {
    flush,
    reportError(source: ErrorSource, error: unknown, pathname: string) {
      const route = routeGroup(pathname);
      const kind = errorClass(error);
      const key = `${source}|${route}|${kind}`;
      if (errors >= MAX_ERRORS_PER_LOAD || seenErrors.has(key)) return;
      errors += 1;
      seenErrors.add(key);
      enqueue({ type: "error", source, route, class: kind });
    },
    reportVital(name: string, value: number, id: string, pathname: string) {
      if (!VITALS.has(name) || seenVitals.has(id) || !Number.isFinite(value) || value < 0) return;
      seenVitals.add(id);
      enqueue({
        type: "vital",
        name: name as VitalName,
        route: routeGroup(pathname),
        value: Math.min(value, name === "CLS" ? MAX_CLS : MAX_TIMING_MS),
      });
    },
  };
}

function defaultSchedule(flush: () => void, delayMs: number) {
  setTimeout(flush, delayMs);
}

/** The keepalive text/plain sender, or null when this build must not report. */
export function browserSender(
  configuration: { status: string; apiBaseUrl: string | null } = publicConfiguration(),
  e2eFixture = process.env.NEXT_PUBLIC_E2E_FIXTURE === "1",
  fetchImpl: typeof fetch | null = typeof fetch === "function" ? fetch : null,
): ((body: string) => void) | null {
  if (e2eFixture || configuration.status !== "ready" || !configuration.apiBaseUrl || !fetchImpl)
    return null;
  const url = new URL("/v1/client-events", configuration.apiBaseUrl).toString();
  return (body) => {
    // text/plain is CORS-safelisted, so no preflight; keepalive lets page-hide batches finish.
    void fetchImpl(url, {
      method: "POST",
      body,
      keepalive: true,
      credentials: "omit",
      headers: { "Content-Type": "text/plain;charset=UTF-8" },
    }).catch(() => undefined);
  };
}

let shared: ReturnType<typeof createClientEventReporter> | null = null;

/** The page's one reporter, created on first use. */
export function clientEvents() {
  if (!shared) {
    shared = createClientEventReporter({
      send: typeof window === "undefined" ? null : browserSender(),
    });
  }
  return shared;
}

/** Reports an error caught by a route or global error boundary. */
export function reportBoundaryError(source: "route_boundary" | "global_boundary", error: unknown) {
  try {
    if (typeof window !== "undefined")
      clientEvents().reportError(source, error, window.location.pathname);
  } catch {
    // Telemetry is best effort.
  }
}
