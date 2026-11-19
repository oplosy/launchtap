"use client";

import { useEffect, useRef, useState } from "react";
import { docsEmblems } from "./docs-emblems";
import { useActiveSection } from "./use-active-section";

/**
 * The docs' right margin: a Greek key band that draws itself down the page as the reader scrolls
 * (it reaches the middle of the screen), and a line emblem beside the illustrated sections, drawn
 * in once the reader reaches that section. Decorative only, and shown on wide screens only.
 */
export function DocsMarginalia({ sections }: { sections: ReadonlyArray<{ id: string }> }) {
  const active = useActiveSection(sections);
  const rootRef = useRef<HTMLDivElement>(null);
  // Each emblem sits level with its section heading, measured from the top of the margin.
  const [offsets, setOffsets] = useState<Readonly<Record<string, number>>>({});
  useEffect(() => {
    const root = rootRef.current;
    if (!root) return;
    const measure = () => {
      const top = root.getBoundingClientRect().top;
      const next: Record<string, number> = {};
      for (const id of Object.keys(docsEmblems)) {
        const heading = document.getElementById(id)?.querySelector("h2");
        if (!heading) continue;
        const box = heading.getBoundingClientRect();
        next[id] = box.top + box.height / 2 - top;
      }
      setOffsets(next);
    };
    let frame = 0;
    const reveal = () => {
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(() => {
        const box = root.getBoundingClientRect();
        const drawn = Math.min(Math.max(window.innerHeight / 2 - box.top, 0), box.height);
        root.style.setProperty("--docs-reveal", `${Math.round(drawn)}px`);
      });
    };
    measure();
    reveal();
    // The margin stretches with the article, so a change in its size moves the headings too.
    const resize = new ResizeObserver(() => {
      measure();
      reveal();
    });
    resize.observe(root);
    window.addEventListener("scroll", reveal, { passive: true });
    return () => {
      resize.disconnect();
      window.removeEventListener("scroll", reveal);
      cancelAnimationFrame(frame);
    };
  }, []);
  const activeIndex = sections.findIndex(({ id }) => id === active);
  return (
    <div className="docs-marginalia" ref={rootRef} aria-hidden="true">
      <svg className="docs-meander" focusable="false">
        <defs>
          {/* One turn of the key, in pixels; the spiral hangs off the line at x = 20. */}
          <pattern id="docs-meander-key" width="24" height="18" patternUnits="userSpaceOnUse">
            <path d="M20 0.5H4.5V14.5H15.5V4.5H8.5V10.5H12" />
          </pattern>
        </defs>
        <rect width="100%" height="100%" fill="url(#docs-meander-key)" />
        <line x1="0.5" x2="0.5" y1="0" y2="100%" />
        <line x1="20" x2="20" y1="0" y2="100%" />
        <line x1="23.5" x2="23.5" y1="0" y2="100%" />
      </svg>
      {sections.map(({ id }, index) => {
        const paths = docsEmblems[id];
        const top = offsets[id];
        if (!paths || top === undefined) return null;
        return (
          <svg
            key={id}
            className="docs-emblem"
            viewBox="0 0 64 64"
            focusable="false"
            style={{ top }}
            data-drawn={index <= activeIndex ? "" : undefined}
            data-active={id === active ? "" : undefined}
          >
            <circle className="docs-emblem-ring" cx="32" cy="32" r="31" />
            <g transform="translate(9 9) scale(0.72)">
              {paths.map((d) => (
                <path key={d} d={d} pathLength={1} />
              ))}
            </g>
          </svg>
        );
      })}
    </div>
  );
}
