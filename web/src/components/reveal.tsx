"use client";

import { useEffect, useRef, type ReactNode } from "react";

/**
 * Whether a reveal starts hidden. Content is hidden only when an observer will reveal it and motion
 * is allowed, so it can never stay invisible (no observer, reduced motion, print).
 */
export function revealInitialState(env: {
  hasObserver: boolean;
  reducedMotion: boolean;
}): "visible" | "pending" {
  return env.hasObserver && !env.reducedMotion ? "pending" : "visible";
}

/** Fades and lifts its content into view once, the first time it scrolls into the viewport. */
export function Reveal({ children, delay = 0 }: { children: ReactNode; delay?: number }) {
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const element = ref.current;
    if (!element) return;
    const state = revealInitialState({
      hasObserver: typeof IntersectionObserver !== "undefined",
      reducedMotion: window.matchMedia("(prefers-reduced-motion: reduce)").matches,
    });
    if (state === "visible") return;
    element.dataset.reveal = "pending";
    const observer = new IntersectionObserver(
      (entries) => {
        if (!entries.some((entry) => entry.isIntersecting)) return;
        delete element.dataset.reveal;
        observer.disconnect();
      },
      { rootMargin: "0px 0px -8% 0px" },
    );
    observer.observe(element);
    return () => {
      observer.disconnect();
      delete element.dataset.reveal;
    };
  }, []);
  return (
    <div ref={ref} className="reveal" style={delay ? { transitionDelay: `${delay}ms` } : undefined}>
      {children}
    </div>
  );
}
