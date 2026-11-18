# Pre-flight: first-party client error and Web Vitals telemetry

Date: 2026-11-18. Source: gap G10 in `docs/audits/2026-11-18-project-gap-assessment.md`
("client-side errors are invisible"). The owner delegated the decision on 2026-11-18. It replaces
the 2026-11-06 deferral of error tracking (`docs/specs/2026-11-06-observability-design.md`).

## Problem

The web app reports nothing. A route that throws in one browser, a chunk that fails to load
after a deploy, or a page whose LCP regresses on mobile is only found when a user complains.
Backend metrics cannot see any of it.

## Locked decisions

1. **First-party, metrics only, no new dependency.** The browser posts error and Web Vitals
   events to the API. The API turns them into Prometheus series and stores nothing else. No
   third-party tracker (Sentry and similar): it would ship visitor data to another company, add
   a dependency to the wallet-carrying bundle, and need a CSP exception. Vitals come from Next's
   built-in `useReportWebVitals`.
   *Trade-off:* no stack traces or messages. An error is counted by where it happened and its
   class, and the operator reproduces it. This is accepted because messages and stacks can
   carry addresses, URLs and user input.

2. **Only closed-set values cross the boundary.** Each event carries enums and one number:
   - error: `source` (`route_boundary`, `global_boundary`, `window_error`,
     `unhandled_rejection`), `route` group, and error `class`. The class is one of `Error`,
     `TypeError`, `ReferenceError`, `RangeError`, `SyntaxError`, `ChunkLoadError` or `other`.
   - vital: `name` (`LCP`, `INP`, `CLS`, `FCP`, `TTFB`), `route` group, and `value`.

   The route group is the first path segment, mapped to the web app's top-level routes, else
   `other`. No URL, address, message, user agent or identifier is sent.
   *Why:* bounded label cardinality (at most a few hundred series), and nothing personal to
   protect.

3. **Endpoint `POST /v1/client-events`, unauthenticated, CORS-simple.**
   - The body is JSON sent as `text/plain` (raw body), so the browser sends it without a CORS
     preflight and with `fetch(..., {keepalive: true, credentials: "omit"})`. Vitals that are
     final only at page hide (INP, CLS) still arrive.
   - It answers `204`.
   - Bounds:
     - a body of at most 8 KiB and at most 20 events;
     - unknown fields, an unknown enum, or a value outside its range (0–60,000 ms, CLS 0–10)
       reject the whole batch with `400`.

   *Alternative rejected:* `application/json` with a preflight, because keepalive requests
   that need a preflight are not reliable across browsers.

4. **Abuse bounds, not authentication.** Anyone can post, so a forged client could inflate the
   counters. The bounds:
   - the request's `Origin` must be one of the API's allowed origins, which keeps other sites'
     visitors out;
   - a per-client limit of 30 batches per minute (`SubjectLimiter`), on top of the global
     limiter.

   Rejections on this route are not counted as write rejections, so beacon abuse cannot fire
   `WriteRejectionSpike`.
   The alerts below are `ticket` severity and only tell an operator to look, never act
   automatically. *Trade-off:* a determined attacker with many IPs can still raise a false
   ticket. That is accepted for advisory telemetry.

5. **Series and alerts.**
   - `launchpad_web_client_errors_total{source,route,class}`;
   - `launchpad_web_vital_seconds{name,route}` histogram for LCP, INP, FCP and TTFB;
   - `launchpad_web_cls{route}` histogram.

   Alerts:
   - `WebClientErrorsHigh` fires when errors exceed 5 % of page loads over 30 minutes, with at
     least 20 errors. TTFB samples count as page loads (one per load).
   - `WebLCPSlow` fires when the mobile-agnostic p75 LCP is over 2.5 s for an hour, with at
     least 50 samples. This matches the target in `docs/specs/2026-11-14-performance-targets.md`.

   Both are unit-tested with `promtool`.

6. **Web reporting rules.**
   - Off in E2E fixture builds, which keeps unexpected requests out of the Playwright suites.
     Off when no API base URL is configured.
   - The page queues events and flushes them 5 s after the first one, or on `pagehide` or a
     hidden tab.
   - At most 10 error events per page load, and each (source, route, class) is reported once
     per load.
   - A failed send is dropped silently; reporting never throws and never logs.

## Out of scope

- Session replay, breadcrumbs and source-mapped stacks.
- Server-side Next errors: they already reach the container logs.
- Per-release labels: they would grow cardinality with every deploy.
