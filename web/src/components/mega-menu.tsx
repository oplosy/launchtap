"use client";

import Link from "next/link";
import { useEffect, useRef, type RefObject } from "react";
import { focusLeftMenu } from "./menu-focus";

/** The text links of the primary navigation, in order. Explore also opens the mega menu. */
export const NAV_LINKS: ReadonlyArray<{ href: string; label: string }> = [
  { href: "/explore", label: "Explore" },
  { href: "/forum", label: "Forum" },
  { href: "/leaderboard", label: "Leaderboard" },
  { href: "/analytics", label: "Analytics" },
  { href: "/docs", label: "Docs" },
];

export const EXPLORE_LINKS: ReadonlyArray<{ href: string; label: string; summary: string }> = [
  { href: "/explore?view=new", label: "New", summary: "Launches as they happen" },
  { href: "/explore?view=trending", label: "Trending", summary: "Where trading is right now" },
  { href: "/explore?view=graduated", label: "Graduated", summary: "Tokens trading on Uniswap" },
];

export const EXPLORE_RESOURCES: ReadonlyArray<{ href: string; label: string }> = [
  { href: "/explore?view=watchlist", label: "Watchlist" },
  { href: "/leaderboard", label: "Leaderboard" },
  { href: "/docs", label: "Docs" },
];

/**
 * Full-width Explore menu under the nav. Escape closes it and returns focus to the trigger;
 * focus leaving both the trigger and the panel closes it; the dimmed backdrop closes it on click.
 */
export function MegaMenu({
  id,
  open,
  onClose,
  triggerRef,
}: {
  id: string;
  open: boolean;
  onClose: () => void;
  triggerRef: RefObject<HTMLElement | null>;
}) {
  const panelRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!open) return;
    const onKey = (event: KeyboardEvent) => {
      if (event.key !== "Escape") return;
      event.preventDefault();
      onClose();
      triggerRef.current?.focus();
    };
    const onFocus = (event: FocusEvent) => {
      if (focusLeftMenu(event.target as Node | null, [triggerRef.current, panelRef.current])) {
        onClose();
      }
    };
    document.addEventListener("keydown", onKey);
    document.addEventListener("focusin", onFocus);
    return () => {
      document.removeEventListener("keydown", onKey);
      document.removeEventListener("focusin", onFocus);
    };
  }, [open, onClose, triggerRef]);

  return (
    <>
      <div
        className="mega-backdrop"
        data-open={open ? "true" : "false"}
        onClick={onClose}
        aria-hidden="true"
      />
      <div
        ref={panelRef}
        id={id}
        className="mega-menu"
        role="region"
        aria-label="Explore menu"
        data-open={open ? "true" : "false"}
        // Closed: CSS makes it visibility:hidden after the fade, which also removes its links
        // from the tab order and the accessibility tree.
      >
        <div className="mega-menu-inner">
          <div className="mega-menu-group">
            <p className="mega-menu-label">Explore</p>
            <ul>
              {EXPLORE_LINKS.map((link) => (
                <li key={link.href}>
                  <Link className="mega-menu-link" href={link.href as never} onClick={onClose}>
                    <span>{link.label}</span>
                    <span className="mega-menu-summary">{link.summary}</span>
                  </Link>
                </li>
              ))}
              <li>
                <Link className="mega-menu-all" href={"/explore" as never} onClick={onClose}>
                  All launches →
                </Link>
              </li>
            </ul>
          </div>
          <div className="mega-menu-group">
            <p className="mega-menu-label">Resources</p>
            <ul>
              {EXPLORE_RESOURCES.map((link) => (
                <li key={link.href}>
                  <Link className="mega-menu-resource" href={link.href as never} onClick={onClose}>
                    {link.label}
                  </Link>
                </li>
              ))}
            </ul>
          </div>
        </div>
      </div>
    </>
  );
}
