"use client";

import { usePathname } from "next/navigation";
import Link from "next/link";
import { lazy, Suspense, useCallback, useEffect, useState, useSyncExternalStore } from "react";
import { motion, useScroll } from "motion/react";
import { MagnifyingGlass } from "@phosphor-icons/react";
import {
  ArrowSquareOut,
  BookOpenText,
  ChartLineUp,
  Compass,
  List,
  Moon,
  Plus,
  ShieldCheck,
  Sun,
  UserCircle,
} from "./icons";
import { SafeExternalLink, Sheet } from "./primitives";
import {
  originOf,
  setThemePreference,
  useResolvedTheme,
  useThemePreference,
  type ThemePreference,
} from "./theme";
import { WalletConnectButton } from "@/wallet/connection-context";
import { publicConfiguration } from "@/config/public";

// The palette is only downloaded the first time someone opens it.
const CommandPalette = lazy(() => import("./command-palette"));

const navItems = [
  { href: "/", label: "Explore", icon: Compass },
  { href: "/create", label: "Create", icon: Plus },
  { href: "/analytics", label: "Analytics", icon: ChartLineUp },
  { href: "/docs", label: "Docs", icon: BookOpenText },
  { href: "/profile", label: "Profile", icon: UserCircle },
] as const;

const noSubscription = () => () => undefined;

export function AppShell({ children }: { children: React.ReactNode }) {
  const pathname = usePathname();
  const configuration = publicConfiguration();
  const [menuOpen, setMenuOpen] = useState(false);
  const [paletteOpen, setPaletteOpen] = useState(false);
  const [paletteLoaded, setPaletteLoaded] = useState(false);
  const [scrolled, setScrolled] = useState(false);
  const { scrollYProgress } = useScroll();
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
  const active = (href: string) => (href === "/" ? pathname === "/" : pathname.startsWith(href));
  return (
    <div className="app-frame">
      <header
        className={`app-navbar ${scrolled ? "is-scrolled" : ""}`}
        aria-label="Primary navigation"
      >
        <div className="navbar-inner">
          <Link className="navbar-brand" href="/" aria-label="Open home">
            <BrandMark />
            <span className="brand-word">launchpad</span>
          </Link>
          <nav className="navbar-links" aria-label="Desktop primary navigation">
            {navItems
              .filter(({ href }) => href !== "/profile")
              .map(({ href, label }) => (
                <Link
                  key={href}
                  href={href}
                  className={`navbar-link ${active(href) ? "is-active" : ""}`}
                  aria-current={active(href) ? "page" : undefined}
                >
                  {active(href) ? (
                    <motion.span
                      className="navbar-link-indicator"
                      layoutId="navbar-indicator"
                      transition={{ type: "spring", stiffness: 480, damping: 38 }}
                      aria-hidden="true"
                    />
                  ) : null}
                  <span>{label}</span>
                </Link>
              ))}
          </nav>
          <div className="navbar-actions">
            <button
              type="button"
              className="navbar-search"
              onClick={openPalette}
              aria-label="Search"
              aria-keyshortcuts="Control+K Meta+K"
            >
              <MagnifyingGlass size={15} aria-hidden="true" />
              <span className="navbar-search-label">Search</span>
              <kbd aria-hidden="true">{modifier} K</kbd>
            </button>
            <ThemeToggle />
            <Link
              className="ui-icon-button navbar-profile"
              href="/profile"
              aria-label="Open profile"
            >
              <UserCircle size={19} />
            </Link>
            <WalletConnectButton />
            <button
              className="ui-icon-button navbar-menu-button"
              onClick={() => setMenuOpen(true)}
              aria-label="Open navigation"
            >
              <List size={20} />
            </button>
          </div>
        </div>
        <motion.span
          className="scroll-progress"
          style={{ scaleX: scrollYProgress }}
          aria-hidden="true"
        />
      </header>
      <main className="app-content">
        {children}
        <footer className="app-footer" aria-label="Site footer">
          <div className="footer-top">
            <div className="footer-brand">
              <span className="footer-name">
                <BrandMark />
                Launchpad
              </span>
              <p>
                Fixed-supply token launches on Robinhood Chain. Your selected wallet signs every
                transaction; Launchpad never takes custody.
              </p>
            </div>
            <nav className="footer-navigation" aria-label="Footer navigation">
              <span>Product</span>
              <Link href="/">Explore</Link>
              <Link href="/create">Create</Link>
              <Link href="/analytics">Analytics</Link>
              <Link href="/profile">Profile</Link>
            </nav>
            <div className="footer-navigation">
              <span>Resources</span>
              <Link href="/docs">Docs</Link>
              <Link href="/docs#risk">Risk and finality</Link>
              {configuration.deployment?.explorerBase ? (
                <SafeExternalLink href={configuration.deployment.explorerBase}>
                  Explorer <ArrowSquareOut size={12} />
                </SafeExternalLink>
              ) : (
                <span className="ui-disabled-link">Explorer unavailable</span>
              )}
            </div>
            <div className="footer-appearance">
              <span>Appearance</span>
              <ThemeSwitcher />
            </div>
          </div>
          <div className="footer-bottom">
            <p className="footer-meta">© 2026 Launchpad</p>
            <p className="footer-risk">
              On-chain transactions may be irreversible. Tokens can be volatile or lose all value.
            </p>
          </div>
        </footer>
      </main>
      <Sheet open={menuOpen} title="Navigate" onClose={() => setMenuOpen(false)}>
        <nav className="sheet-nav" aria-label="Mobile menu navigation">
          {navItems.map(({ href, label, icon: Icon }, index) => (
            <Link
              key={href}
              href={href}
              onClick={() => setMenuOpen(false)}
              className={active(href) ? "is-active" : ""}
              style={{ "--i": index } as React.CSSProperties}
            >
              <Icon size={20} />
              {label}
            </Link>
          ))}
        </nav>
        <div className="sheet-appearance">
          <span>Appearance</span>
          <ThemeSwitcher />
        </div>
        <div className="sheet-note">
          <ShieldCheck size={18} />
          <p>
            Wallet connections and signing remain user-controlled. Launchpad never takes custody of
            funds.
          </p>
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

function BrandMark() {
  return (
    <svg className="brand-mark" viewBox="0 0 32 32" aria-hidden="true">
      <path className="brand-mark-axis" d="M5 5v22h22" />
      <path className="brand-mark-curve" d="M7 25C15 24 21 19 25.5 8" pathLength={1} />
      <circle className="brand-mark-dot" cx="25.5" cy="8" r="2.6" />
    </svg>
  );
}

function ThemeToggle() {
  const resolved = useResolvedTheme();
  const next = resolved === "dark" ? "light" : "dark";
  return (
    <button
      type="button"
      className="ui-icon-button theme-toggle"
      onClick={(event) => setThemePreference(next, originOf(event.currentTarget))}
      aria-label={`Switch to ${next} theme`}
      title={`Switch to ${next} theme`}
    >
      <Sun className="theme-icon-sun" size={17} aria-hidden="true" />
      <Moon className="theme-icon-moon" size={17} aria-hidden="true" />
    </button>
  );
}

const THEME_OPTIONS: ReadonlyArray<{ value: ThemePreference; label: string }> = [
  { value: "system", label: "System" },
  { value: "light", label: "Light" },
  { value: "dark", label: "Dark" },
];

function ThemeSwitcher() {
  const preference = useThemePreference();
  const index = THEME_OPTIONS.findIndex((option) => option.value === preference);
  return (
    <div
      className="theme-switcher"
      role="group"
      aria-label="Color theme"
      style={{ "--index": index } as React.CSSProperties}
    >
      <span className="theme-switcher-indicator" aria-hidden="true" />
      {THEME_OPTIONS.map((option) => (
        <button
          key={option.value}
          type="button"
          aria-pressed={preference === option.value}
          onClick={(event) => setThemePreference(option.value, originOf(event.currentTarget))}
        >
          {option.label}
        </button>
      ))}
    </div>
  );
}
