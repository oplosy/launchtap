"use client";

import { useEffect, useId, useRef, useState } from "react";
import { docsSideShown } from "./docs-visibility";
import { useActiveSection } from "./use-active-section";

/** Table of contents: beside the text on wide screens, behind a "Contents" button below 1024 px. */
export function DocsNav({ sections }: { sections: ReadonlyArray<{ id: string; label: string }> }) {
  const active = useActiveSection(sections);
  const [open, setOpen] = useState(false);
  const navId = useId();
  const contentsRef = useRef<HTMLDivElement>(null);
  // Out of and back into the fog with the margin line (see `docsSideShown`). Only the wide layout
  // uses it (CSS ignores it below 1024 px).
  const [shown, setShown] = useState(false);
  useEffect(() => {
    const contents = contentsRef.current;
    const layout = contents?.parentElement;
    if (!contents || !layout) return;
    // Stick at the vertical centre of the screen, below the nav when the screen is short.
    const place = () => {
      const nav = document.querySelector(".app-navbar")?.getBoundingClientRect().height ?? 0;
      const centre = (window.innerHeight - contents.offsetHeight) / 2;
      contents.style.setProperty("--docs-contents-top", `${Math.max(nav + 16, centre)}px`);
    };
    const sync = () => setShown(docsSideShown(layout));
    place();
    sync();
    window.addEventListener("scroll", sync, { passive: true });
    window.addEventListener("resize", place);
    return () => {
      window.removeEventListener("scroll", sync);
      window.removeEventListener("resize", place);
    };
  }, []);
  return (
    <div className="docs-contents" ref={contentsRef} data-shown={shown ? "true" : "false"}>
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
        {sections.map(({ id, label }) => (
          <a
            key={id}
            href={`#${id}`}
            aria-current={active === id ? "location" : undefined}
            onClick={() => setOpen(false)}
          >
            {label}
          </a>
        ))}
      </nav>
    </div>
  );
}
