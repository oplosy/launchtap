"use client";

import { useEffect, useId, useRef, useState } from "react";

/** Table of contents: beside the text on wide screens, behind a "Contents" button below 1024 px. */
export function DocsNav({ sections }: { sections: ReadonlyArray<{ id: string; label: string }> }) {
  const [active, setActive] = useState(sections[0]?.id);
  const [open, setOpen] = useState(false);
  const navId = useId();
  const contentsRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    // Stick at the list's own starting height instead of sliding up under the nav: the layout
    // grid's top is where the list first renders. Clamped so a tall list still fits the screen.
    const contents = contentsRef.current;
    const layout = contents?.parentElement;
    if (!contents || !layout) return;
    const place = () => {
      const start = layout.getBoundingClientRect().top + window.scrollY;
      const fit = window.innerHeight - contents.offsetHeight - 24;
      contents.style.setProperty("--docs-contents-top", `${Math.max(0, Math.min(start, fit))}px`);
    };
    place();
    window.addEventListener("resize", place);
    return () => window.removeEventListener("resize", place);
  }, []);
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
    <div className="docs-contents" ref={contentsRef}>
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
            <span className="mono">{String(index + 1).padStart(2, "0")}</span>
            {label}
          </a>
        ))}
      </nav>
    </div>
  );
}
