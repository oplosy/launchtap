"use client";

import { useEffect, useId, useRef, useState } from "react";

/** Table of contents: beside the text on wide screens, behind a "Contents" button below 1024 px. */
export function DocsNav({ sections }: { sections: ReadonlyArray<{ id: string; label: string }> }) {
  const [active, setActive] = useState(sections[0]?.id);
  const [open, setOpen] = useState(false);
  const navId = useId();
  const contentsRef = useRef<HTMLDivElement>(null);
  // Hidden at the top of the page, faded in on the first scroll, faded out once the end of the
  // article reaches the list. Only the wide layout uses it (CSS ignores it below 1024 px).
  const [shown, setShown] = useState(false);
  useEffect(() => {
    const contents = contentsRef.current;
    const article = contents?.parentElement?.querySelector(".docs-article");
    if (!contents || !article) return;
    // Stick at the vertical centre of the screen, below the nav when the screen is short.
    const place = () => {
      const nav = document.querySelector(".app-navbar")?.getBoundingClientRect().height ?? 0;
      const centre = (window.innerHeight - contents.offsetHeight) / 2;
      contents.style.setProperty("--docs-contents-top", `${Math.max(nav + 16, centre)}px`);
    };
    const sync = () => {
      // The sticky list reaches the article's last line exactly when it would start scrolling
      // away; fade a little before that so it dissolves in place instead of sliding off.
      const ended =
        article.getBoundingClientRect().bottom <= contents.getBoundingClientRect().bottom + 96;
      setShown(window.scrollY > 0 && !ended);
    };
    place();
    sync();
    window.addEventListener("scroll", sync, { passive: true });
    window.addEventListener("resize", place);
    return () => {
      window.removeEventListener("scroll", sync);
      window.removeEventListener("resize", place);
    };
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
