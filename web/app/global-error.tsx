"use client";

import { useEffect } from "react";
import { reportBoundaryError } from "@/telemetry/client-events";

export default function GlobalError({
  error,
  reset,
}: {
  error: Error & { digest?: string };
  reset: () => void;
}) {
  useEffect(() => {
    reportBoundaryError("global_boundary", error);
  }, [error]);
  return (
    <html lang="en">
      <body>
        <main style={{ maxWidth: 680, margin: "10vh auto", padding: 24, fontFamily: "system-ui" }}>
          <h1>Launchpad is temporarily unavailable.</h1>
          <p>Reload the application to restore the public read-only shell.</p>
          <button onClick={reset}>Reload</button>
        </main>
      </body>
    </html>
  );
}
