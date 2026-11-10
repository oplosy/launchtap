"use client";

import { useEffect, useState } from "react";
import { ApiClient } from "@/api/client";
import { useWatchlist } from "./use-watchlist";
import { displayWatchCount, watchCountLabel } from "./watch-count-model";

/** "N watching" for one token; renders nothing until the count loads or if it fails. */
export function WatchCount({ address, apiBaseUrl }: { address: string; apiBaseUrl: string }) {
  const watchlist = useWatchlist();
  const [count, setCount] = useState<number | null>(null);
  const [baseline, setBaseline] = useState<boolean | null>(null);

  useEffect(() => {
    const controller = new AbortController();
    void new ApiClient({ baseUrl: apiBaseUrl })
      .getTokenWatchers(address, controller.signal)
      .then((body) => {
        const value = body?.watch_count;
        if (!controller.signal.aborted && Number.isSafeInteger(value) && value >= 0)
          setCount(value);
      })
      .catch(() => undefined);
    return () => controller.abort();
  }, [address, apiBaseUrl]);

  const listReady = watchlist.status === "ready";
  const starred = watchlist.has(address);
  // Record, once, whether the server count already included this reader (React's
  // "adjust state while rendering" pattern; it settles in one extra render).
  if (count !== null && baseline === null && listReady) setBaseline(starred);

  if (count === null) return null;
  return (
    <span className="watch-count" aria-live="polite">
      {watchCountLabel(displayWatchCount(count, baseline, listReady, starred))}
    </span>
  );
}
