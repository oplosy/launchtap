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
  const eth = useTransform(progress, [0.25, 0.55], [0, GRADUATION_ETH]);
  const point = useTransform(eth, curveAt);
  const dotX = useTransform(point, (value) => chartCoordinates(value, CHART.w, CHART.h)[0]);
  const dotY = useTransform(point, (value) => chartCoordinates(value, CHART.w, CHART.h)[1]);
  const drawnWidth = useTransform(dotX, (x) => x + 10);
  const dotOpacity = useTransform(progress, [0.23, 0.27], [0, 1]);
  const threshold = useTransform(progress, [0.5, 0.58], [0, 1]);
  const pulseR = useTransform(progress, [0.56, 0.7], [6, 72]);
  const pulseOpacity = useTransform(progress, [0.56, 0.58, 0.7], [0, 0.9, 0]);
  const stamp = useTransform(progress, [0.58, 0.64], [0, 1]);
  const stampScale = useTransform(progress, [0.58, 0.64], [1.18, 1]);
  const chartFade = useTransform(progress, [0.16, 0.26, 0.74, 0.8], [0, 1, 1, 0.06]);
  const pool = useTransform(progress, [0.76, 0.82], [0, 1]);
  const ethCoinX = useTransform(progress, [0.78, 0.88], [60, 228]);
  const tokenCoinX = useTransform(progress, [0.78, 0.88], [500, 332]);
  const lpOpacity = useTransform(progress, [0.86, 0.89, 0.93, 0.95], [0, 1, 1, 0]);
  const lpY = useTransform(progress, [0.87, 0.93], [300, 396]);
  const burnBox = useTransform(progress, [0.84, 0.9], [0, 1]);
  const burned = useTransform(progress, [0.94, 0.98], [0, 1]);
  const unburned = useTransform(burned, (value) => 1 - value);
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

      <motion.g className="story-chart" style={{ opacity: chartFade }}>
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
          <motion.line
            className="story-threshold"
            x1={CHART.w}
            x2={CHART.w}
            y1={0}
            y2={CHART.h}
            style={{ opacity: threshold }}
          />
          <path className="story-curve-ghost" d={PATH} />
          <path className="story-curve" d={PATH} clipPath="url(#story-drawn)" />
          <motion.circle
            className="story-dot"
            cx={dotX}
            cy={dotY}
            r={6}
            style={{ opacity: dotOpacity }}
          />
          <motion.circle
            className="story-pulse"
            cx={CHART.w}
            cy={0}
            r={pulseR}
            style={{ opacity: pulseOpacity }}
          />
          <g transform={`translate(${CHART.w - 204} 30)`}>
            <motion.g style={{ opacity: stamp, scale: stampScale, originX: 1, originY: 0.5 }}>
              <rect className="story-stamp" width={180} height={30} rx={15} />
              <text className="story-stamp-text" x={90} y={19} textAnchor="middle">
                Graduated · 4.2 ETH
              </text>
            </motion.g>
          </g>
        </g>
      </motion.g>

      <motion.g style={{ opacity: pool }}>
        <rect className="story-pool" x={160} y={186} width={240} height={128} rx={20} />
        <text x={280} y={214} textAnchor="middle" className="story-pool-title">
          Uniswap v2 pool
        </text>
        <text x={280} y={232} textAnchor="middle" className="story-label">
          WETH / TOKEN
        </text>
      </motion.g>
      <motion.g style={{ opacity: pool, x: ethCoinX }}>
        <circle className="story-coin" cx={0} cy={270} r={28} />
        <text x={0} y={274} textAnchor="middle" className="story-coin-text">
          4.2 ETH
        </text>
      </motion.g>
      <motion.g style={{ opacity: pool, x: tokenCoinX }}>
        <circle className="story-coin is-token" cx={0} cy={270} r={28} />
        <text x={0} y={274} textAnchor="middle" className="story-coin-text">
          200M
        </text>
      </motion.g>

      <motion.g style={{ opacity: burnBox }}>
        <rect className="story-burn" x={190} y={380} width={180} height={44} rx={22} />
        <motion.text
          x={280}
          y={406}
          textAnchor="middle"
          className="story-burn-text"
          style={{ opacity: unburned }}
        >
          0x…dEaD
        </motion.text>
        <motion.text
          x={280}
          y={406}
          textAnchor="middle"
          className="story-burn-text is-burned"
          style={{ opacity: burned }}
        >
          LP burned · locked forever
        </motion.text>
      </motion.g>
      <motion.g style={{ opacity: lpOpacity, y: lpY }}>
        <rect className="story-lp-chip" x={244} y={-13} width={72} height={26} rx={13} />
        <text x={280} y={4} textAnchor="middle" className="story-lp-chip-text">
          LP
        </text>
      </motion.g>
    </svg>
  );
}
