"use client";

import { useEffect, useRef, useState } from "react";

/**
 * Below 1024 px the trade card opens as a modal sheet from the bottom bar; the side picks its
 * tab. Focus is trapped inside the sheet and returns to the opener when it closes.
 */
export function useTradeSheet() {
  const [sheetSide, setSheetSide] = useState<"buy" | "sell" | null>(null);
  const asideRef = useRef<HTMLElement>(null);
  useEffect(() => {
    if (!sheetSide) return;
    const opener = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    const close = () => setSheetSide(null);
    const aside = asideRef.current;
    // Land on the requested side's tab; re-selecting the active side would clear the amount.
    const tab =
      aside?.querySelectorAll<HTMLButtonElement>('[role="tab"]')[sheetSide === "buy" ? 0 : 1];
    if (tab && !tab.disabled && tab.getAttribute("aria-selected") !== "true") tab.click();
    else
      (tab && !tab.disabled
        ? tab
        : aside?.querySelector<HTMLElement>(".token-sheet-close")
      )?.focus();
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        event.preventDefault();
        close();
        return;
      }
      const current = asideRef.current;
      if (event.key !== "Tab" || !current) return;
      // Focus stays inside the sheet while it is modal.
      const focusable = Array.from(
        current.querySelectorAll<HTMLElement>(
          'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])',
        ),
      ).filter((element) => element.getClientRects().length > 0);
      const first = focusable[0];
      const last = focusable.at(-1);
      if (!first || !last) return;
      if (!current.contains(document.activeElement)) {
        event.preventDefault();
        first.focus();
      } else if (event.shiftKey && document.activeElement === first) {
        event.preventDefault();
        last.focus();
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault();
        first.focus();
      }
    };
    // The sheet only exists below 1024 px; widening the window returns the card to the column.
    const wide = window.matchMedia("(min-width: 1024px)");
    const overflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    document.addEventListener("keydown", onKeyDown);
    wide.addEventListener("change", close);
    return () => {
      document.body.style.overflow = overflow;
      document.removeEventListener("keydown", onKeyDown);
      wide.removeEventListener("change", close);
      opener?.focus();
    };
  }, [sheetSide]);
  return { sheetSide, setSheetSide, asideRef };
}
