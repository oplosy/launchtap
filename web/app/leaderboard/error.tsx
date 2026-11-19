"use client";

import { useEffect } from "react";
import Link from "next/link";
import { UnavailableState } from "@/components/primitives";
import { scrubTelemetry, safeLog } from "@/security/logging";
import { reportBoundaryError } from "@/telemetry/client-events";

/**
 * Shown only when the leaderboard has no good render to fall back on: the page throws on an API
 * failure so a previously generated page or cached ranking keeps being served instead.
 */
export default function LeaderboardError({
  error,
  retry,
}: {
  error: Error & { digest?: string };
  retry: () => void;
}) {
  useEffect(() => {
    safeLog("route_error", { digest: error.digest, error: scrubTelemetry(error) });
    reportBoundaryError("route_boundary", error);
  }, [error]);
  return (
    // `retry` (Next 16.3+) refetches the server content; `reset` would only re-render it.
    <div className="page-stack leaderboard-page">
      <h1 className="sr-only">Leaderboard</h1>
      <UnavailableState
        title="Leaderboard unavailable"
        description="The ranking could not be loaded. Try again in a minute."
      />
      <div className="error-actions">
        <button className="ui-button ui-button-secondary ui-button-md" onClick={() => retry()}>
          Try again
        </button>
        <Link className="ui-button ui-button-secondary ui-button-md" href="/analytics">
          Back to Analytics
        </Link>
      </div>
    </div>
  );
}
