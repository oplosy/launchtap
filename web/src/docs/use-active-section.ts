"use client";

import { useEffect, useState } from "react";

/** The docs section being read: the one crossing the upper third of the viewport. */
export function useActiveSection(sections: ReadonlyArray<{ id: string }>) {
  const [active, setActive] = useState(sections[0]?.id);
  useEffect(() => {
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
  return active;
}
