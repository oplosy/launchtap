"use client";

import { useEffect, useRef, useState } from "react";
import { docsEmblems } from "./docs-emblems";
import { useActiveSection } from "./use-active-section";

const WIDTH = 72;
const MIDDLE = WIDTH / 2;
const SWAY = 30;
const SEED = 0x5eed;

type Point = readonly [number, number];

/** mulberry32: a small seeded generator, so the line wanders the same way on every render. */
function seeded(seed: number) {
  let state = seed >>> 0;
  return () => {
    state = (state + 0x6d2b79f5) >>> 0;
    let t = state;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

/**
 * Where the line passes on its way down: through the middle at the top, at every emblem and at the
 * bottom, and in between at uneven steps and uneven distances from the middle. It mostly swaps
 * sides but sometimes leans the same way twice, and now and then hooks back up before going on.
 */
function threadPoints(height: number, stops: readonly number[]) {
  const anchors = [0, ...stops.filter((y) => y > 0 && y < height).sort((a, b) => a - b), height];
  const next = seeded(SEED);
  const points: Point[] = [[MIDDLE, 0]];
  let side = next() < 0.5 ? -1 : 1;
  const swing = () => {
    if (next() < 0.78) side = -side;
    return MIDDLE + side * (5 + next() * (SWAY - 5));
  };
  for (let i = 1; i < anchors.length; i += 1) {
    const to = anchors[i] ?? 0;
    let y = anchors[i - 1] ?? 0;
    for (;;) {
      const advance = 34 + next() * 118;
      if (to - y - advance < 36) break;
      y += advance;
      points.push([swing(), y]);
      if (next() < 0.2) points.push([swing(), y - (12 + next() * 26)]);
    }
    points.push([MIDDLE, to]);
  }
  return points;
}

/** A smooth line through the points (Catmull-Rom, as cubic Béziers). */
function threadPath(height: number, stops: readonly number[]) {
  const points = threadPoints(height, stops);
  const at = (i: number) => points[Math.min(Math.max(i, 0), points.length - 1)] ?? [MIDDLE, 0];
  const n = (v: number) => v.toFixed(1);
  let d = `M${MIDDLE} 0`;
  for (let i = 0; i < points.length - 1; i += 1) {
    const [x0, y0] = at(i - 1);
    const [x1, y1] = at(i);
    const [x2, y2] = at(i + 1);
    const [x3, y3] = at(i + 2);
    d += `C${n(x1 + (x2 - x0) / 6)} ${n(y1 + (y2 - y0) / 6)} ${n(x2 - (x3 - x1) / 6)} ${n(y2 - (y3 - y1) / 6)} ${n(x2)} ${n(y2)}`;
  }
  return d;
}

/**
 * The docs' right margin: a thin line that wanders down the page as the reader scrolls (drawn down
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
