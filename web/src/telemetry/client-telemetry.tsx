"use client";

import { useReportWebVitals } from "next/web-vitals";
import { useEffect } from "react";
import { clientEvents } from "./client-events";

type ReportedMetric = { name: string; value: number; id: string; navigationURL?: string };

/** The path the metric was measured on: a soft-navigation metric can arrive after the URL moved. */
export function metricPathname(metric: Pick<ReportedMetric, "navigationURL">, fallback: string) {
  try {
    return metric.navigationURL ? new URL(metric.navigationURL, "http://x").pathname : fallback;
  } catch {
    return fallback;
  }
}

function reportWebVital(metric: ReportedMetric) {
  try {
    const pathname = metricPathname(metric, window.location.pathname);
    clientEvents().reportVital(metric.name, metric.value, metric.id, pathname);
  } catch {
    // Telemetry is best effort.
  }
}

/**
 * True for errors thrown by this origin's scripts. Extension and cross-origin errors are ignored,
 * including sanitized "Script error." events, which carry no filename.
 */
export function sameOriginError(event: Pick<ErrorEvent, "filename">, origin: string) {
  return event.filename.startsWith(`${origin}/`);
}

/** Mounts once in the root layout: Web Vitals, uncaught errors and unhandled rejections. */
export function ClientTelemetry() {
  useReportWebVitals(reportWebVital);
  useEffect(() => {
    const reporter = clientEvents();
    const onError = (event: ErrorEvent) => {
      if (sameOriginError(event, window.location.origin))
        reporter.reportError("window_error", event.error, window.location.pathname);
    };
    const onRejection = (event: PromiseRejectionEvent) =>
      reporter.reportError("unhandled_rejection", event.reason, window.location.pathname);
    const onHide = () => reporter.flush();
    const onVisibility = () => {
      if (document.visibilityState === "hidden") reporter.flush();
    };
    window.addEventListener("error", onError);
    window.addEventListener("unhandledrejection", onRejection);
    window.addEventListener("pagehide", onHide);
    document.addEventListener("visibilitychange", onVisibility);
    return () => {
      window.removeEventListener("error", onError);
      window.removeEventListener("unhandledrejection", onRejection);
      window.removeEventListener("pagehide", onHide);
      document.removeEventListener("visibilitychange", onVisibility);
    };
  }, []);
  return null;
}
