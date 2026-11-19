"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { docsEmblems } from "./docs-emblems";
import { docsSideShown } from "./docs-visibility";
import { useActiveSection } from "./use-active-section";

/** An emblem's medallion, in px. */
const EMBLEM = 72;
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
 * Where the line passes on its way down the margin: long, uneven steps that drift across the
 * column by uneven amounts, so every bend is a slow one, and every emblem strung on it at its
 * heading.
 * Returns the points and each emblem's horizontal centre.
 */
function threadLayout(width: number, height: number, stops: Readonly<Record<string, number>>) {
  const next = seeded(SEED);
  const pick = (low: number, high: number) => low + next() * (high - low);
  const edge = 12;
  const inner = Math.min(EMBLEM / 2 + 6, width / 2);
  const emblemX: Record<string, number> = {};
  const targets: [string | null, number][] = [
    ...Object.entries(stops)
      .filter(([, y]) => y > 0 && y < height)
      .sort((a, b) => a[1] - b[1]),
    [null, height],
  ];
  const targetX = targets.map(([id]) =>
    id ? pick(inner, width - inner) : pick(edge, width - edge),
  );
  // Start straight above the first stop: the first emblem sits right at the top, and a start
  // anywhere else would draw a near-flat stroke across to it.
  const points: Point[] = [[targetX[0] ?? width / 2, 0]];
  let right = next() < 0.5;
  for (const [index, [id, to]] of targets.entries()) {
    let y = points[points.length - 1]?.[1] ?? 0;
    for (;;) {
      const advance = pick(190, 330);
      if (to - y - advance < 140) break;
      y += advance;
      if (next() < 0.8) right = !right;
      points.push([right ? pick(width * 0.6, width - edge) : pick(edge, width * 0.4), y]);
    }
    const x = targetX[index] ?? width / 2;
    if (id) emblemX[id] = x;
    points.push([x, to]);
  }
  return { points, emblemX };
}

/**
 * A smooth line through the points: centripetal Catmull-Rom, as cubic Béziers. Centripetal spacing
 * keeps uneven steps from overshooting into kinks, so every turn stays a soft bend.
 */
function smoothPath(points: readonly Point[]) {
  const at = (i: number) => points[Math.min(Math.max(i, 0), points.length - 1)] ?? [0, 0];
  const n = (v: number) => v.toFixed(1);
  const knot = (a: Point, b: Point) => Math.sqrt(Math.hypot(b[0] - a[0], b[1] - a[1]));
  const [startX, startY] = at(0);
  let d = `M${n(startX)} ${n(startY)}`;
  for (let i = 0; i < points.length - 1; i += 1) {
    const p0 = at(i - 1);
    const p1 = at(i);
    const p2 = at(i + 1);
    const p3 = at(i + 2);
    const d1 = knot(p0, p1);
    const d2 = knot(p1, p2);
    const d3 = knot(p2, p3);
    const control = (from: Point, to: Point, back: Point, k: number, step: number, axis: 0 | 1) =>
      k < 1e-6
        ? from[axis] + (to[axis] - from[axis]) / 3
        : (k * k * to[axis] -
            step * step * back[axis] +
            (2 * k * k + 3 * k * step + step * step) * from[axis]) /
          (3 * k * (k + step));
    d +=
      `C${n(control(p1, p2, p0, d1, d2, 0))} ${n(control(p1, p2, p0, d1, d2, 1))}` +
      ` ${n(control(p2, p1, p3, d3, d2, 0))} ${n(control(p2, p1, p3, d3, d2, 1))}` +
      ` ${n(p2[0])} ${n(p2[1])}`;
  }
  return d;
}

/**
 * The docs' right margin: a thin line that wanders down the page as the reader scrolls (drawn down
 * to the middle of the screen), with a line emblem strung on it level with each illustrated
 * section, drawn in once the reader reaches that section. It surfaces out of a blur on the first
 * scroll and dissolves into one at the end, with the contents list. Decorative, wide screens only.
 */
export function DocsMarginalia({ sections }: { sections: ReadonlyArray<{ id: string }> }) {
  const active = useActiveSection(sections);
  const rootRef = useRef<HTMLDivElement>(null);
  // Each emblem sits level with its section heading, measured from the top of the margin.
  const [offsets, setOffsets] = useState<Readonly<Record<string, number>>>({});
  const [size, setSize] = useState({ width: 0, height: 0 });
  const [shown, setShown] = useState(false);
  useEffect(() => {
    const root = rootRef.current;
    const layout = root?.parentElement;
    if (!root || !layout) return;
    const measure = () => {
      const { top, width, height } = root.getBoundingClientRect();
      setSize({ width: Math.round(width), height: Math.round(height) });
      const next: Record<string, number> = {};
      for (const id of Object.keys(docsEmblems)) {
        const heading = document.getElementById(id)?.querySelector("h2");
        if (!heading) continue;
        const box = heading.getBoundingClientRect();
        next[id] = box.top + box.height / 2 - top;
      }
      setOffsets(next);
    };
    // The drawn end follows the middle of the screen with an ease, so the line glides on after a
    // scroll instead of jumping with each wheel step.
    const still = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    let frame = 0;
    let drawn: number | null = null;
    const glide = () => {
      const box = root.getBoundingClientRect();
      const target = Math.min(Math.max(window.innerHeight / 2 - box.top, 0), box.height);
      drawn = drawn === null || still ? target : drawn + (target - drawn) * 0.12;
      if (Math.abs(target - drawn) < 0.5) drawn = target;
      root.style.setProperty("--docs-reveal", `${drawn.toFixed(1)}px`);
      setShown(docsSideShown(layout));
      frame = drawn === target ? 0 : requestAnimationFrame(glide);
    };
    const reveal = () => {
      if (!frame) frame = requestAnimationFrame(glide);
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
  const thread = useMemo(
    () => (size.height > 0 ? threadLayout(size.width, size.height, offsets) : null),
    [size, offsets],
  );
  const activeIndex = sections.findIndex(({ id }) => id === active);
  return (
    <div
      className="docs-marginalia"
      ref={rootRef}
      aria-hidden="true"
      data-shown={shown ? "true" : "false"}
    >
      {thread ? (
        <svg className="docs-thread" focusable="false">
          <path d={smoothPath(thread.points)} />
        </svg>
      ) : null}
      {sections.map(({ id }, index) => {
        const paths = docsEmblems[id];
        const top = offsets[id];
        const x = thread?.emblemX[id];
        if (!paths || top === undefined || x === undefined) return null;
        return (
          <svg
            key={id}
            className="docs-emblem"
            viewBox="0 0 64 64"
            focusable="false"
            style={{ top, left: x - EMBLEM / 2 }}
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
