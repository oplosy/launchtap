"use client";

import { useEffect, useRef } from "react";

const ACTIVE_SELECTORS = [
  '[aria-pressed="true"]',
  '[aria-selected="true"]',
  '[aria-current="page"]',
  ".is-active",
];

/**
 * One shared highlight for a group of choices (segments, tabs, chips). Place it as the last child
 * of the group: it finds the active sibling, follows it when the selection or layout changes, and
 * the group's CSS decides whether it reads as a pill or an underline. The first placement does not
 * animate; every later change slides.
 */
export function SlidingIndicator() {
  const ref = useRef<HTMLSpanElement>(null);
  useEffect(() => {
    const indicator = ref.current;
    const group = indicator?.parentElement;
    if (!indicator || !group) return;
    let frame = 0;
    const place = () => {
      const active = Array.from(group.children).find(
        (child): child is HTMLElement =>
          child !== indicator &&
          child instanceof HTMLElement &&
          ACTIVE_SELECTORS.some((selector) => child.matches(selector)),
      );
      if (!active) {
        indicator.removeAttribute("data-placed");
        return;
      }
      indicator.style.setProperty("--x", `${active.offsetLeft}px`);
      indicator.style.setProperty("--y", `${active.offsetTop}px`);
      indicator.style.setProperty("--w", `${active.offsetWidth}px`);
      indicator.style.setProperty("--h", `${active.offsetHeight}px`);
      if (!indicator.hasAttribute("data-placed")) {
        indicator.setAttribute("data-placed", "");
        // Transitions switch on one frame later, so the first position is not animated in.
        frame = requestAnimationFrame(() => indicator.setAttribute("data-ready", ""));
      }
    };
    place();
    const selection = new MutationObserver(place);
    selection.observe(group, {
      subtree: true,
      attributes: true,
      attributeFilter: ["aria-pressed", "aria-selected", "aria-current", "class"],
    });
    const layout = new ResizeObserver(place);
    layout.observe(group);
    for (const child of Array.from(group.children)) {
      if (child !== indicator) layout.observe(child);
    }
    return () => {
      cancelAnimationFrame(frame);
      selection.disconnect();
      layout.disconnect();
    };
  }, []);
  return <span ref={ref} className="slide-indicator" aria-hidden="true" />;
}
