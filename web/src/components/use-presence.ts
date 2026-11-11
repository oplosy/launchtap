"use client";

import { useEffect, useState } from "react";

/**
 * Keeps a closing surface mounted for its exit animation. Render while it returns true and mark
 * the element with `data-state={open ? "open" : "closed"}`; CSS animates both states.
 */
export function usePresence(open: boolean, exitMs = 200): boolean {
  const [mounted, setMounted] = useState(open);
  if (open && !mounted) setMounted(true);
  useEffect(() => {
    if (open) return;
    const timer = window.setTimeout(() => setMounted(false), exitMs);
    return () => window.clearTimeout(timer);
  }, [open, exitMs]);
  return mounted;
}
