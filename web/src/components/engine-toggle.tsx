"use client";

import { useState } from "react";
import { SlidingIndicator } from "./sliding-indicator";

const ENGINES = ["V1", "V2"] as const;

/**
 * Launch engine switch on the create page. Only engine v1 exists today (notes.md, 2026-09-01):
 * the choice is visual only and the form below launches through v1 whichever is picked.
 */
export function EngineToggle() {
  const [engine, setEngine] = useState<(typeof ENGINES)[number]>("V1");
  return (
    <div className="engine-toggle" role="group" aria-label="Launch engine">
      {ENGINES.map((option) => (
        <button
          key={option}
          type="button"
          aria-pressed={engine === option}
          onClick={() => setEngine(option)}
        >
          {option}
        </button>
      ))}
      <SlidingIndicator />
    </div>
  );
}
