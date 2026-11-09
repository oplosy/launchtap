/**
 * Runs `run` now, or once more after the current run when triggered while one is in flight.
 * Never rejects: a reconnect reload aborts an in-flight refresh (an expected AbortError), and any
 * other failure is already surfaced by the loaders' own error state, so neither may escape as an
 * unhandled rejection or reach the error UI from here.
 */
export function createCoalescedRefresh(run: () => Promise<void>): () => void {
  let running = false;
  let again = false;
  return () => {
    if (running) {
      again = true;
      return;
    }
    running = true;
    void (async () => {
      try {
        do {
          again = false;
          try {
            await run();
          } catch {
            // See above: aborted by a newer load, or already shown by the loader that failed. A
            // follow-up queued meanwhile still runs.
          }
        } while (again);
      } finally {
        running = false;
      }
    })();
  };
}
