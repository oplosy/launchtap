import { useRef } from "react";

/**
 * Guards async review preparation: every close (a side, amount, slippage or wallet change) bumps
 * the generation, so a preparation that began before the change cannot reopen a stale review.
 */
export function useReviewGeneration() {
  const generation = useRef(0);
  return {
    bump: () => {
      generation.current += 1;
    },
    begin: <Intent>(open: (intent: Intent) => void) => {
      const started = generation.current;
      const current = () => generation.current === started;
      return {
        current,
        /** Opens the review only if nothing changed since `begin`; reports whether it did. */
        open: (intent: Intent) => {
          if (!current()) return false;
          open(intent);
          return true;
        },
      };
    },
  };
}
