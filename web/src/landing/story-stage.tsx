import { motion, useTransform, type MotionValue } from "motion/react";
import { GRADUATION_ETH, chartCoordinates, curveAt, curvePath } from "./curve";

const W = 560;
const CHART = { x: 36, y: 118, w: 500, h: 280 };
const PATH = curvePath(CHART.w, CHART.h);
const CURVE_SHARE = 0.8;

/** Scroll-driven illustration for LaunchStory; `progress` runs 0 → 1 across the section. */
export function StoryStage({ progress }: { progress: MotionValue<number> }) {
  const curveFill = useTransform(progress, [0.02, 0.14], [0, 1]);
  const lpReveal = useTransform(progress, [0.08, 0.18], [0, 1]);
  const axes = useTransform(progress, [0.16, 0.26], [0, 1]);
  const eth = useTransform(progress, [0.25, 0.55], [0, GRADUATION_ETH]);
  const point = useTransform(eth, curveAt);
  const dotX = useTransform(point, (value) => chartCoordinates(value, CHART.w, CHART.h)[0]);
  const dotY = useTransform(point, (value) => chartCoordinates(value, CHART.w, CHART.h)[1]);
  const drawnWidth = useTransform(dotX, (x) => x + 10);
  const dotOpacity = useTransform(progress, [0.23, 0.27], [0, 1]);
  const ethLabel = useTransform(eth, (value) => `${value.toFixed(2)} ETH`);
  return (
    <svg className="story-svg" viewBox={`0 0 ${W} 470`}>
      <defs>
        <clipPath id="story-drawn">
          <motion.rect x={-10} y={-20} height={CHART.h + 40} width={drawnWidth} />
        </clipPath>
        <pattern id="story-hatch" width="6" height="6" patternUnits="userSpaceOnUse">
          <path d="M0 6L6 0" className="story-hatch-line" />
        </pattern>
      </defs>

      <g className="story-supply">
        <text x={0} y={12} className="story-label">
          Supply · 1,000,000,000
        </text>
        <rect className="story-track" x={0} y={26} width={W} height={14} rx={7} />
        <motion.rect
          className="story-curve-share"
          x={0}
          y={26}
          width={W * CURVE_SHARE}
          height={14}
          rx={7}
          style={{ scaleX: curveFill, originX: 0 }}
        />
        <motion.g style={{ opacity: lpReveal }}>
          <rect
            className="story-lp-share"
            x={W * CURVE_SHARE + 3}
            y={26}
            width={W * (1 - CURVE_SHARE) - 3}
            height={14}
            rx={7}
            fill="url(#story-hatch)"
          />
          <text x={W} y={64} textAnchor="end" className="story-label">
            200M · LP reserve
          </text>
        </motion.g>
        <motion.text x={0} y={64} className="story-label" style={{ opacity: curveFill }}>
          800M · bonding curve
        </motion.text>
      </g>

      <motion.g className="story-chart" style={{ opacity: axes }}>
        <g transform={`translate(${CHART.x} ${CHART.y})`}>
          <line className="story-axis" x1={0} x2={0} y1={0} y2={CHART.h} />
          <line className="story-axis" x1={0} x2={CHART.w} y1={CHART.h} y2={CHART.h} />
          <text x={-8} y={8} textAnchor="end" className="story-label">
            16×
          </text>
          <text x={-8} y={CHART.h} textAnchor="end" className="story-label">
            1×
          </text>
          <text x={CHART.w} y={CHART.h + 22} textAnchor="end" className="story-label">
            800M sold
          </text>
          <motion.text x={CHART.w} y={-14} textAnchor="end" className="story-counter">
            {ethLabel}
          </motion.text>
          <path className="story-curve-ghost" d={PATH} />
          <path className="story-curve" d={PATH} clipPath="url(#story-drawn)" />
          <motion.circle
            className="story-dot"
            cx={dotX}
            cy={dotY}
            r={6}
            style={{ opacity: dotOpacity }}
          />
        </g>
      </motion.g>
    </svg>
  );
}
