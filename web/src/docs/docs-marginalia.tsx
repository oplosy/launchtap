"use client";

import { useEffect, useRef, useState } from "react";
import { docsEmblems } from "./docs-emblems";
import { useActiveSection } from "./use-active-section";

const WIDTH = 72;
const MIDDLE = WIDTH / 2;
const SWAY = 26;
const STRIDE = 96;

/**
 * A thin line that slaloms down the margin, crossing the middle at every emblem: between two
 * stops it makes an even number of half-waves, alternating sides, each a cubic whose controls sit
 * 4/3 of the sway out so the turn peaks at exactly `SWAY` and joins its neighbour smoothly.
 */
function threadPath(height: number, stops: readonly number[]) {
  const points = [0, ...stops.filter((y) => y > 0 && y < height).sort((a, b) => a - b), height];
  let d = `M${MIDDLE} 0`;
  let side = 1;
  for (let i = 1; i < points.length; i += 1) {
    const from = points[i - 1] ?? 0;
    const to = points[i] ?? 0;
    const turns = Math.max(2, 2 * Math.round((to - from) / STRIDE / 2));
    const step = (to - from) / turns;
    for (let k = 0; k < turns; k += 1) {
      const a = from + k * step;
      const x = MIDDLE + (side * SWAY * 4) / 3;
      d += `C${x.toFixed(1)} ${(a + step / 4).toFixed(1)} ${x.toFixed(1)} ${(a + (3 * step) / 4).toFixed(1)} ${MIDDLE} ${(a + step).toFixed(1)}`;
      side = -side;
    }
  }
  return d;
}

/**
 * The docs' right margin: a thin line that slaloms down the page as the reader scrolls (drawn down
 * to the middle of the screen), and a line emblem beside the illustrated sections, drawn in once
 * the reader reaches that section. Decorative only, and shown on wide screens only.
 */
export function DocsMarginalia({ sections }: { sections: ReadonlyArray<{ id: string }> }) {
  const active = useActiveSection(sections);
  const rootRef = useRef<HTMLDivElement>(null);
  // Each emblem sits level with its section heading, measured from the top of the margin.
  const [offsets, setOffsets] = useState<Readonly<Record<string, number>>>({});
  const [height, setHeight] = useState(0);
  useEffect(() => {
    const root = rootRef.current;
    if (!root) return;
    const measure = () => {
      const { top, height: rootHeight } = root.getBoundingClientRect();
      setHeight(Math.round(rootHeight));
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
      {height > 0 ? (
        <svg className="docs-thread" focusable="false">
          <path d={threadPath(height, Object.values(offsets))} />
        </svg>
      ) : null}
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
