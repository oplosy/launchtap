"use client";

import { usePathname } from "next/navigation";
import Link from "next/link";
import {
  Fragment,
  lazy,
  Suspense,
  useCallback,
  useEffect,
  useId,
  useRef,
  useState,
  useSyncExternalStore,
} from "react";
import { MagnifyingGlass } from "@phosphor-icons/react";
import { List } from "./icons";
import { Sheet } from "./primitives";
import { MegaMenu, NAV_LINKS } from "./mega-menu";
import { BackToTop } from "./back-to-top";
import { SiteFooter } from "./site-footer";
import { ThemeToggle } from "./theme-toggle";
import { walletPath } from "@/portfolio/wallet-model";
import { WalletConnectButton, useWalletConnection } from "@/wallet/connection-context";
import { NotificationBell } from "@/live/notifications";

// The palette is only downloaded the first time someone opens it.
const CommandPalette = lazy(() => import("./command-palette"));

const noSubscription = () => () => undefined;

export function AppShell({ children }: { children: React.ReactNode }) {
  const pathname = usePathname();
  // The lightweight connection context only: it reports an address once a wallet is connected.
  const connection = useWalletConnection();
  const sheetLinks: ReadonlyArray<{ href: string; label: string }> = [
    ...NAV_LINKS,
    { href: "/create", label: "Create" },
    { href: "/profile", label: "Profile" },
    ...(connection.connected && connection.address
      ? [{ href: walletPath(connection.address), label: "My wallet" }]
      : []),
  ];
  const [menuOpen, setMenuOpen] = useState(false);
  const [exploreOpen, setExploreOpen] = useState(false);
  const exploreRef = useRef<HTMLButtonElement>(null);
  const exploreMenuId = useId();
  const [paletteOpen, setPaletteOpen] = useState(false);
  const [paletteLoaded, setPaletteLoaded] = useState(false);
  const [scrolled, setScrolled] = useState(false);
  const modifier = useSyncExternalStore(
    noSubscription,
    () => (/Mac|iPhone|iPad/.test(navigator.userAgent) ? "⌘" : "Ctrl"),
    () => "⌘",
  );
  const openPalette = useCallback(() => {
    setPaletteLoaded(true);
    setPaletteOpen(true);
  }, []);
  const closePalette = useCallback(() => setPaletteOpen(false), []);
  const closeExplore = useCallback(() => setExploreOpen(false), []);
  useEffect(() => {
    const sync = () => setScrolled(window.scrollY > 8);
    sync();
    window.addEventListener("scroll", sync, { passive: true });
    return () => window.removeEventListener("scroll", sync);
  }, []);
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      const typing =
        event.target instanceof HTMLElement &&
        (event.target.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(event.target.tagName));
      if ((event.key === "k" || event.key === "K") && (event.metaKey || event.ctrlKey)) {
        event.preventDefault();
        openPalette();
      } else if (event.key === "/" && !typing) {
        event.preventDefault();
        openPalette();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [openPalette]);
  // A route change closes the menus (their links also close them on click).
  const [shownPath, setShownPath] = useState(pathname);
  if (shownPath !== pathname) {
    setShownPath(pathname);
    setExploreOpen(false);
  }
  const active = (href: string) => pathname === href || pathname.startsWith(`${href}/`);
  // Hover intent: leaving the menu closes it only after a short grace period, so a fast move from
  // the trigger down into the still-unrolling panel (or a brief overshoot) does not shut it.
  const closeTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const cancelHoverClose = useCallback(() => {
    if (closeTimer.current) clearTimeout(closeTimer.current);
    closeTimer.current = null;
  }, []);
  const scheduleHoverClose = useCallback(() => {
    // Already counting down: further moves off the menu must not push the close back.
    if (closeTimer.current) return;
    closeTimer.current = setTimeout(() => {
      closeTimer.current = null;
      setExploreOpen(false);
    }, 80);
  }, []);
  useEffect(() => cancelHoverClose, [cancelHoverClose]);
  const hoverOpen = (event: React.PointerEvent) => {
    if (event.pointerType !== "mouse") return;
    cancelHoverClose();
    setExploreOpen(true);
  };
  const hoverClose = (event: React.PointerEvent) => {
    if (event.pointerType !== "mouse") return;
    cancelHoverClose();
    setExploreOpen(false);
  };
  const hoverLeave = (event: React.PointerEvent) => {
    if (event.pointerType === "mouse") scheduleHoverClose();
  };
  return (
    <div className="app-frame">
      <header
        className={`app-navbar${scrolled ? " is-scrolled" : ""}${exploreOpen ? " is-menu-open" : ""}`}
        onPointerLeave={hoverLeave}
      >
        <div className="navbar-inner">
          <Link className="navbar-brand" href="/" aria-label="Open home">
            Launchpad
          </Link>
          <nav className="navbar-links" aria-label="Primary">
            {NAV_LINKS.map(({ href, label }) =>
              href === "/explore" ? (
                // The panel follows its trigger in DOM order so Tab moves straight into it.
                <Fragment key={href}>
                  <button
                    ref={exploreRef}
                    type="button"
                    className={`navbar-link${active(href) ? " is-active" : ""}`}
                    aria-expanded={exploreOpen}
                    aria-controls={exploreMenuId}
                    // A mouse click lands right after the hover that opened the menu, so it keeps
                    // the menu open; keyboard activation (detail 0) still toggles it.
                    onClick={(event) => setExploreOpen((open) => (event.detail > 0 ? true : !open))}
                    onPointerEnter={hoverOpen}
                  >
                    {label}
                  </button>
                  <MegaMenu
                    id={exploreMenuId}
                    open={exploreOpen}
                    onClose={closeExplore}
                    onHoverLeave={scheduleHoverClose}
                    onHoverReturn={cancelHoverClose}
                    triggerRef={exploreRef}
                  />
                </Fragment>
              ) : (
                <Link
                  key={href}
                  href={href as never}
                  className={`navbar-link${active(href) ? " is-active" : ""}`}
                  aria-current={active(href) ? "page" : undefined}
                  onPointerEnter={hoverClose}
                >
                  {label}
                </Link>
              ),
            )}
          </nav>
          <div className="navbar-actions">
            <button
              type="button"
              className="ui-icon-button navbar-search"
              onClick={openPalette}
              aria-label="Search"
              aria-keyshortcuts="Control+K Meta+K"
              title={`Search (${modifier} K)`}
            >
              <MagnifyingGlass size={18} aria-hidden="true" />
            </button>
            <ThemeToggle />
            <NotificationBell />
            <Link
              className="ui-button ui-button-secondary ui-button-sm navbar-create"
              href="/create"
            >
              Create
            </Link>
            <WalletConnectButton variant="primary" />
            <button
              type="button"
              className="ui-icon-button navbar-menu-button"
              onClick={() => setMenuOpen(true)}
              aria-label="Open menu"
            >
              <List size={20} />
            </button>
          </div>
        </div>
      </header>
      <main className="app-content">{children}</main>
      <SiteFooter />
      <BackToTop />
      <Sheet open={menuOpen} title="Menu" onClose={() => setMenuOpen(false)}>
        <nav className="sheet-nav" aria-label="Menu">
          {sheetLinks.map(({ href, label }) => (
            <Link
              key={href}
              href={href as never}
              onClick={() => setMenuOpen(false)}
              aria-current={active(href) ? "page" : undefined}
            >
              {label}
            </Link>
          ))}
        </nav>
        <div className="sheet-foot">
          <button
            type="button"
            className="ui-button ui-button-secondary"
            onClick={() => {
              setMenuOpen(false);
              openPalette();
            }}
          >
            Search
          </button>
        </div>
      </Sheet>
      {paletteLoaded ? (
        <Suspense fallback={null}>
          <CommandPalette open={paletteOpen} onClose={closePalette} />
        </Suspense>
      ) : null}
    </div>
  );
}
