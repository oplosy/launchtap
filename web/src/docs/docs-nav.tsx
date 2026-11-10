"use client";

import { useEffect, useId, useState } from "react";

/** Table of contents: beside the text on wide screens, behind a "Contents" button below 1024 px. */
export function DocsNav({ sections }: { sections: ReadonlyArray<{ id: string; label: string }> }) {
  const [active, setActive] = useState(sections[0]?.id);
  const [open, setOpen] = useState(false);
  const navId = useId();
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
    <div className="docs-contents">
      <button
        type="button"
        className="ui-button ui-button-secondary ui-button-md docs-contents-toggle"
        aria-expanded={open}
        aria-controls={navId}
        onClick={() => setOpen((value) => !value)}
      >
        Contents
      </button>
      <nav
        id={navId}
        className="docs-nav"
        aria-label="Documentation sections"
        data-open={open ? "" : undefined}
      >
        {sections.map(({ id, label }, index) => (
          <a
            key={id}
            href={`#${id}`}
            aria-current={active === id ? "location" : undefined}
            onClick={() => setOpen(false)}
          >
            <span className="mono">0{index + 1}</span>
            {label}
          </a>
        ))}
      </nav>
    </div>
  );
}
