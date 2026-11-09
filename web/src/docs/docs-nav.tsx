"use client";

import { useEffect, useState } from "react";
import { motion } from "motion/react";

export function DocsNav({ sections }: { sections: ReadonlyArray<{ id: string; label: string }> }) {
  const [active, setActive] = useState(sections[0]?.id);
  useEffect(() => {
    // The section crossing the upper third of the viewport is the one being read.
    const observer = new IntersectionObserver(
      (entries) => {
        const visible = entries.find((entry) => entry.isIntersecting);
        if (visible) setActive(visible.target.id);
      },
      { rootMargin: "-30% 0px -65% 0px" },
    );
    for (const { id } of sections) {
      const element = document.getElementById(id);
      if (element) observer.observe(element);
    }
    return () => observer.disconnect();
  }, [sections]);
  return (
    <aside className="workspace-panel docs-nav" aria-label="Documentation sections">
      {sections.map(({ id, label }, index) => (
        <a
          key={id}
          href={`#${id}`}
          className={active === id ? "is-active" : undefined}
          aria-current={active === id ? "location" : undefined}
        >
          {active === id ? (
            <motion.span
              className="docs-nav-indicator"
              layoutId="docs-nav-indicator"
              transition={{ type: "spring", stiffness: 420, damping: 38 }}
            />
          ) : null}
          <span className="mono">0{index + 1}</span>
          {label}
        </a>
      ))}
    </aside>
  );
}
