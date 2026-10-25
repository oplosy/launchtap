"use client";

import Link from "next/link";
import { motion, useReducedMotion } from "motion/react";
import { ArrowDown, ArrowRight } from "@phosphor-icons/react";
import { CurveInstrument } from "./curve-instrument";

const HEADLINE = ["One curve", "from launch", "to liquidity."];
const FACTS = [
  { value: "1B", label: "Fixed supply, minted once" },
  { value: "1%", label: "Fee on every curve trade" },
  { value: "4.2 ETH", label: "Raised to graduate" },
  { value: "100%", label: "Of the initial LP burned" },
];
const ease = [0.22, 1, 0.36, 1] as const;

export function Hero() {
  const reduce = useReducedMotion();
  const rise = (delay: number) =>
    reduce
      ? {}
      : {
          initial: { opacity: 0, y: 18 },
          animate: { opacity: 1, y: 0 },
          transition: { duration: 0.9, delay, ease },
        };
  return (
    <section className="landing-hero" aria-labelledby="landing-title">
      <div className="landing-hero-copy">
        <h1 id="landing-title" className="landing-title">
          {HEADLINE.map((line, index) => (
            <span className="landing-title-line" key={line}>
              <motion.span
                className="landing-title-inner"
                initial={reduce ? false : { y: "110%" }}
                animate={{ y: "0%" }}
                transition={{ duration: 1.1, delay: 0.1 + index * 0.1, ease }}
              >
                {line}
              </motion.span>
            </span>
          ))}
        </h1>
        <motion.p className="landing-lede" {...rise(0.4)}>
          Every token starts with a fixed 1B supply on a public bonding curve. At 4.2 ETH it
          graduates to Uniswap v2 and its initial liquidity is burned. Your wallet signs every step.
        </motion.p>
        <motion.div className="landing-actions" {...rise(0.5)}>
          <Link className="ui-button ui-button-primary ui-button-lg" href="/create">
            <span>
              Create a launch <ArrowRight size={16} aria-hidden="true" />
            </span>
          </Link>
          <a className="ui-button ui-button-secondary ui-button-lg landing-jump" href="#launches">
            <span>
              Explore launches <ArrowDown size={16} aria-hidden="true" />
            </span>
          </a>
        </motion.div>
      </div>
      <motion.div className="landing-hero-visual" {...rise(0.3)}>
        <CurveInstrument />
      </motion.div>
      <motion.dl className="landing-facts" aria-label="Protocol parameters" {...rise(0.65)}>
        {FACTS.map((fact) => (
          <div key={fact.label}>
            <dt>{fact.label}</dt>
            <dd>{fact.value}</dd>
          </div>
        ))}
      </motion.dl>
    </section>
  );
}
