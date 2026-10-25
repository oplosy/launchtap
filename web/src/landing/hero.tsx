"use client";

import Link from "next/link";
import { motion, useReducedMotion } from "motion/react";

const HEADLINE = ["One curve", "from launch", "to liquidity."];
const FACTS = ["1B fixed supply", "1% curve fee", "Graduates at 4.2 ETH", "Initial LP burned"];
const ease = [0.22, 1, 0.36, 1] as const;

export function Hero({ children }: { children?: React.ReactNode }) {
  const reduce = useReducedMotion();
  const rise = (delay: number) =>
    reduce
      ? {}
      : {
          initial: { opacity: 0, y: 18 },
          animate: { opacity: 1, y: 0 },
          transition: { duration: 0.8, delay, ease },
        };
  return (
    <section className="landing-hero" aria-labelledby="landing-title">
      <div className="landing-hero-copy">
        <motion.p className="section-kicker" {...rise(0)}>
          Fixed supply · Robinhood Chain
        </motion.p>
        <h1 id="landing-title" className="landing-title">
          {HEADLINE.map((line, index) => (
            <span className="landing-title-line" key={line}>
              <motion.span
                className="landing-title-inner"
                initial={reduce ? false : { y: "105%" }}
                animate={{ y: "0%" }}
                transition={{ duration: 1, delay: 0.08 + index * 0.09, ease }}
              >
                {line}
              </motion.span>
            </span>
          ))}
        </h1>
        <motion.p className="landing-lede" {...rise(0.35)}>
          Every token starts with a fixed 1B supply and an 800M bonding curve. At 4.2 ETH it
          graduates to a Uniswap v2 pool and the initial LP position is burned. Your wallet signs
          every step.
        </motion.p>
        <motion.div className="landing-actions" {...rise(0.45)}>
          <Link className="ui-button ui-button-primary ui-button-lg" href="/create">
            Create a launch
          </Link>
          <a className="ui-button ui-button-quiet ui-button-lg landing-jump" href="#launches">
            Browse launches <span aria-hidden="true">↓</span>
          </a>
        </motion.div>
      </div>
      <motion.div className="landing-hero-visual" {...rise(0.25)}>
        {children}
      </motion.div>
      <motion.ul className="landing-facts" aria-label="Protocol parameters" {...rise(0.6)}>
        {FACTS.map((fact) => (
          <li key={fact}>{fact}</li>
        ))}
      </motion.ul>
    </section>
  );
}
