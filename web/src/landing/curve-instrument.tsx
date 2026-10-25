"use client";

import { useEffect, useRef, useState } from "react";
import {
  animate,
  motion,
  useInView,
  useMotionValue,
  useReducedMotion,
  useSpring,
  useTransform,
} from "motion/react";
import {
  GRADUATION_ETH,
  chartCoordinates,
  curveAt,
  curvePath,
  ethForSoldShare,
  formatCompact,
  priceMultiple,
} from "./curve";

const VIEW = { width: 560, height: 380, left: 52, right: 24, top: 24, bottom: 42 };
const PLOT_W = VIEW.width - VIEW.left - VIEW.right;
const PLOT_H = VIEW.height - VIEW.top - VIEW.bottom;
const PATH = curvePath(PLOT_W, PLOT_H);
const Y_TICKS = [1, 4, 8, 12, 16];
const X_TICKS = [0, 200, 400, 600, 800];

export function CurveInstrument() {
  const [value, setValue] = useState(GRADUATION_ETH * 0.62);
  const target = useMotionValue(value);
  const eth = useSpring(target, { stiffness: 260, damping: 34, mass: 0.6 });
  const point = useTransform(eth, curveAt);
  const x = useTransform(point, (value) => chartCoordinates(value, PLOT_W, PLOT_H)[0]);
  const y = useTransform(point, (value) => chartCoordinates(value, PLOT_W, PLOT_H)[1]);
  const raised = useTransform(point, (value) => `${value.ethRaised.toFixed(2)} ETH`);
  const sold = useTransform(point, (value) => formatCompact(value.tokensSold));
  const multiple = useTransform(point, (value) => `${priceMultiple(value).toFixed(2)}×`);
  const percent = useTransform(point, (value) => `${Math.round(value.progress * 100)}%`);
  const progress = useTransform(point, (value) => value.progress);
  const frame = useRef<HTMLElement>(null);
  const inView = useInView(frame);
  const reduce = useReducedMotion();
  const [engaged, setEngaged] = useState(false);
  // Idle demo: ride the curve back and forth until someone takes over.
  useEffect(() => {
    if (reduce || engaged || !inView) return;
    let controls: ReturnType<typeof animate> | undefined;
    const start = window.setTimeout(() => {
      controls = animate(target, [target.get(), GRADUATION_ETH, 0.2, target.get()], {
        duration: 9,
        ease: "easeInOut",
        repeat: Infinity,
      });
    }, 1200);
    return () => {
      window.clearTimeout(start);
      controls?.stop();
    };
  }, [reduce, engaged, inView, target]);
  const scrub = (next: number) => {
    setValue(next);
    target.set(next);
  };
  const scrubToPointer = (event: React.PointerEvent<SVGSVGElement>) => {
    const box = event.currentTarget.getBoundingClientRect();
    const local = ((event.clientX - box.left) / box.width) * VIEW.width - VIEW.left;
    scrub(ethForSoldShare(local / PLOT_W));
  };
  return (
    <figure
      ref={frame}
      className="workspace-panel curve-instrument"
      onPointerEnter={() => setEngaged(true)}
      onPointerLeave={() => setEngaged(false)}
      onFocus={() => setEngaged(true)}
      onBlur={() => setEngaged(false)}
    >
      <figcaption className="curve-head">
        <span className="panel-kicker">V1 bonding curve</span>
        <span className="curve-head-note mono">illustrative · not a quote</span>
      </figcaption>
      <dl className="curve-readout">
        <div>
          <dt>ETH raised</dt>
          <motion.dd className="mono">{raised}</motion.dd>
        </div>
        <div>
          <dt>Tokens sold</dt>
          <motion.dd className="mono">{sold}</motion.dd>
        </div>
        <div>
          <dt>Price vs launch</dt>
          <motion.dd className="mono">{multiple}</motion.dd>
        </div>
        <div>
          <dt>To graduation</dt>
          <motion.dd className="mono">{percent}</motion.dd>
        </div>
      </dl>
      <div className="curve-meter" aria-hidden="true">
        <motion.span style={{ scaleX: progress }} />
      </div>
      <input
        className="curve-range"
        type="range"
        min={0}
        max={GRADUATION_ETH}
        step={0.01}
        value={value}
        onChange={(event) => scrub(Number(event.target.value))}
        aria-label="ETH raised on the curve"
        aria-valuetext={`${value.toFixed(2)} ETH raised, ${Math.round(curveAt(value).progress * 100)}% to graduation`}
      />
      <svg
        className="curve-chart"
        onPointerMove={scrubToPointer}
        onPointerDown={scrubToPointer}
        viewBox={`0 0 ${VIEW.width} ${VIEW.height}`}
        role="img"
        aria-label="Price rises sixteen-fold along the curve as 800 million tokens sell for 4.2 ETH."
      >
        <defs>
          <linearGradient id="curve-fill" x1="0" x2="0" y1="0" y2="1">
            <stop offset="0%" stopColor="var(--accent)" stopOpacity="0.22" />
            <stop offset="100%" stopColor="var(--accent)" stopOpacity="0" />
          </linearGradient>
          <clipPath id="curve-progress">
            <motion.rect x={0} y={-10} height={PLOT_H + 20} width={x} />
          </clipPath>
        </defs>
        <g transform={`translate(${VIEW.left} ${VIEW.top})`}>
          {Y_TICKS.map((tick) => {
            const ty = PLOT_H - ((tick - 1) / 15) * PLOT_H;
            return (
              <g key={tick} className="curve-grid">
                <line x1={0} x2={PLOT_W} y1={ty} y2={ty} />
                <text x={-12} y={ty + 4} textAnchor="end">
                  {tick}×
                </text>
              </g>
            );
          })}
          {X_TICKS.map((tick) => (
            <text
              key={tick}
              className="curve-axis-label"
              x={(tick / 800) * PLOT_W}
              y={PLOT_H + 26}
              textAnchor={tick === 0 ? "start" : tick === 800 ? "end" : "middle"}
            >
              {tick === 0 ? "0" : `${tick}M`}
            </text>
          ))}
          <line className="curve-graduation" x1={PLOT_W} x2={PLOT_W} y1={0} y2={PLOT_H} />
          <path className="curve-line-base" d={PATH} />
          <g clipPath="url(#curve-progress)">
            <path d={`${PATH} L${PLOT_W} ${PLOT_H} L0 ${PLOT_H} Z`} fill="url(#curve-fill)" />
            <path className="curve-line" d={PATH} />
          </g>
          <motion.line className="curve-cross" x1={x} x2={x} y1={y} y2={PLOT_H} />
          <motion.line className="curve-cross" x1={0} x2={x} y1={y} y2={y} />
          <motion.circle className="curve-dot-halo" cx={x} cy={y} r={14} />
          <motion.circle className="curve-dot" cx={x} cy={y} r={5} />
        </g>
      </svg>
    </figure>
  );
}
