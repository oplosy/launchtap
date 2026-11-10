"use client";

import { useEffect } from "react";
import Link from "next/link";
import { scrubTelemetry, safeLog } from "@/security/logging";

export default function Error({
  error,
  reset,
}: {
  error: Error & { digest?: string };
  reset: () => void;
}) {
  useEffect(() => {
    safeLog("route_error", { digest: error.digest, error: scrubTelemetry(error) });
  }, [error]);
  return (
    <div className="page-stack">
      <section className="ui-state ui-error-state" role="alert" aria-labelledby="route-error-title">
        <div>
          <p className="panel-kicker">Route unavailable</p>
          <h1 id="route-error-title">This route could not load.</h1>
          <p>Retry the request, or return to Explore. No wallet or transaction data was saved.</p>
          <div className="error-actions">
            <button className="ui-button ui-button-primary ui-button-md" onClick={reset}>
              Try again
            </button>
            <Link className="ui-button ui-button-secondary ui-button-md" href="/explore">
              Return to Explore
            </Link>
          </div>
        </div>
      </section>
    </div>
  );
}
