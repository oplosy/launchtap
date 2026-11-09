"use client";

import { useRef, useState } from "react";
import {
  motion,
  useMotionValue,
  useMotionValueEvent,
  useReducedMotion,
  useScroll,
  useSpring,
  useTransform,
  type MotionValue,
} from "motion/react";
import { StoryStage } from "./story-stage";

const CHAPTERS = [
  {
    label: "Supply",
    title: "One billion tokens. Fixed at launch.",
    body: "800M are sold on the curve. 200M are held back for the liquidity pool. Nothing is minted later.",
  },
  {
    label: "Curve",
    title: "Price follows the curve, not a market maker.",
    body: "Every buy moves the price up the same virtual-reserve curve. A 1% fee is split between the protocol and the creator.",
  },
  {
    label: "Graduation",
    title: "At 4.2 ETH, the curve closes.",
    body: "The buy that crosses the threshold graduates the token. Curve trading stops for good.",
  },
  {
    label: "Liquidity",
    title: "Liquidity moves to Uniswap v2. The LP is burned.",
    body: "4.2 ETH and the reserved 200M tokens seed the pool. The LP tokens are minted to 0x…dEaD, so nobody can pull that liquidity.",
  },
] as const;

export function LaunchStory() {
  const ref = useRef<HTMLElement>(null);
  const { scrollYProgress } = useScroll({ target: ref, offset: ["start start", "end end"] });
  const progress = useSpring(scrollYProgress, { stiffness: 120, damping: 30, mass: 0.4 });
  // Reduced motion: no pinning, every chapter visible, stage frozen on the graduation frame.
  const reduce = useReducedMotion();
  const still = useMotionValue(0.7);
  const [active, setActive] = useState(0);
  useMotionValueEvent(scrollYProgress, "change", (value) =>
    setActive(Math.min(CHAPTERS.length - 1, Math.floor(value * CHAPTERS.length))),
  );
  return (
    <section
      ref={ref}
      className={`story ${reduce ? "is-static" : ""}`}
      aria-labelledby="story-title"
    >
      <div className="story-sticky">
        <div className="story-copy">
          <p className="section-kicker">How a launch works</p>
          <h2 id="story-title" className="story-title">
            From first buy to burned liquidity.
          </h2>
          <ol className="story-rail">
            {CHAPTERS.map((chapter, index) => (
              <RailItem
                key={chapter.label}
                index={index}
                label={chapter.label}
                progress={progress}
                active={index === active}
              />
            ))}
          </ol>
          <div className="story-chapters">
            {CHAPTERS.map((chapter, index) => (
              <article
                key={chapter.label}
                className={`story-chapter ${index === active ? "is-active" : ""}`}
                data-direction={index < active ? "past" : "future"}
              >
                <h3>{chapter.title}</h3>
                <p>{chapter.body}</p>
              </article>
            ))}
          </div>
        </div>
        <div className="story-stage" aria-hidden="true">
          <StoryStage progress={reduce ? still : progress} />
        </div>
      </div>
    </section>
  );
}

function RailItem({
  index,
  label,
  progress,
  active,
}: {
  index: number;
  label: string;
  progress: MotionValue<number>;
  active: boolean;
}) {
  const fill = useTransform(progress, [index / 4, (index + 1) / 4], [0, 1]);
  return (
    <li className={active ? "is-active" : undefined}>
      <span className="mono">0{index + 1}</span>
      <span>{label}</span>
      <motion.i style={{ scaleX: fill }} />
    </li>
  );
}
