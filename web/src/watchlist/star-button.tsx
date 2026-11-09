"use client";

import { Star } from "@phosphor-icons/react";
import { useEffect } from "react";
import { Toast } from "@/components/primitives";
import { useWatchlist } from "./use-watchlist";

const TOAST_MS = 6_000;

/** Toggles a token on the signed-in reader's watchlist; hidden where sign-in is unavailable. */
export function StarButton({ address, label }: { address: string; label: string }) {
  const watchlist = useWatchlist();
  if (!watchlist.available) return null;
  const starred = watchlist.has(address);
  return (
    <button
      type="button"
      className={`star-button ${starred ? "is-starred" : ""}`}
      aria-pressed={starred}
      aria-label={starred ? `Remove ${label} from watchlist` : `Add ${label} to watchlist`}
      onClick={(event) => {
        event.preventDefault();
        event.stopPropagation();
        void watchlist.toggle(address);
      }}
    >
      <Star aria-hidden="true" size={18} weight={starred ? "fill" : "regular"} />
    </button>
  );
}

/** Renders the shared watchlist failure toast. Mount once per page that shows stars. */
export function WatchlistToaster() {
  const { toast, dismissToast } = useWatchlist();
  const id = toast?.id;
  useEffect(() => {
    if (id === undefined) return;
    const timer = setTimeout(dismissToast, TOAST_MS);
    return () => clearTimeout(timer);
  }, [dismissToast, id]);
  if (!toast) return null;
  return (
    <div className="watchlist-toast">
      <Toast
        title={toast.title}
        description={toast.description}
        tone={toast.tone}
        onDismiss={dismissToast}
      />
    </div>
  );
}
