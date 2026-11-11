"use client";

import { ArrowUp } from "@phosphor-icons/react";
import { useEffect, useState } from "react";

/**
 * Bottom-right "back to top" once the reader is more than a screen down, e.g. after several
 * "Load more" pages. Smooth scroll, or an instant jump under reduced motion.
 */
export function BackToTop() {
  const [shown, setShown] = useState(false);
  useEffect(() => {
    const sync = () => setShown(window.scrollY > window.innerHeight);
    sync();
    window.addEventListener("scroll", sync, { passive: true });
    window.addEventListener("resize", sync);
    return () => {
      window.removeEventListener("scroll", sync);
      window.removeEventListener("resize", sync);
    };
  }, []);
  return (
    <button
      type="button"
      className="back-to-top"
      data-shown={shown ? "true" : "false"}
      aria-label="Back to top"
      // Hidden from keyboard and assistive tech until it is shown.
      tabIndex={shown ? 0 : -1}
      aria-hidden={shown ? undefined : true}
      onClick={() => {
        const reduce = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
        window.scrollTo({ top: 0, behavior: reduce ? "auto" : "smooth" });
      }}
    >
      <ArrowUp size={18} aria-hidden="true" />
    </button>
  );
}
