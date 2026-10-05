"use client";

import { usePathname } from "next/navigation";
import Link from "next/link";
import { useEffect, useState } from "react";
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
import { WalletConnectButton } from "@/wallet/connection-context";
import { publicConfiguration } from "@/config/public";

const navItems = [
  { href: "/", label: "Explore", icon: Compass, primary: false },
  { href: "/create", label: "Create", icon: Plus, primary: true },
  { href: "/analytics", label: "Analytics", icon: ChartLineUp, primary: false },
  { href: "/docs", label: "Docs", icon: BookOpenText, primary: false },
  { href: "/profile", label: "Profile", icon: UserCircle, primary: false },
] as const;

export function AppShell({ children }: { children: React.ReactNode }) {
  const pathname = usePathname();
  const configuration = publicConfiguration();
  const [menuOpen, setMenuOpen] = useState(false);
  const [theme, setTheme] = useState<"dark" | "light">("dark");
  useEffect(() => {
    const saved = window.localStorage.getItem("launchpad-theme");
    const next = saved === "light" ? "light" : "dark";
    document.documentElement.dataset.theme = next;
    if (next === "dark") return;
    const sync = window.setTimeout(() => setTheme(next), 0);
    return () => window.clearTimeout(sync);
  }, []);
  function toggleTheme() {
    const next = theme === "dark" ? "light" : "dark";
    setTheme(next);
    document.documentElement.dataset.theme = next;
    window.localStorage.setItem("launchpad-theme", next);
  }
  const active = (href: string) => (href === "/" ? pathname === "/" : pathname.startsWith(href));
  return (
    <div className="app-frame">
      <header className="app-navbar" aria-label="Primary navigation">
        <Link className="navbar-brand" href="/" aria-label="Open home">
          <span className="brand-orbit" aria-hidden="true" />
        </Link>
        <nav className="navbar-links" aria-label="Desktop primary navigation">
          {navItems
            .filter(({ href }) => href !== "/profile")
            .map(({ href, label, icon: Icon, primary }) => (
              <Link
                key={href}
                href={href}
                className={`navbar-link ${active(href) ? "is-active" : ""} ${primary ? "is-primary" : ""}`}
                aria-current={active(href) ? "page" : undefined}
              >
                <Icon size={18} weight={active(href) ? "fill" : "regular"} aria-hidden="true" />
                <span>{label}</span>
              </Link>
            ))}
        </nav>
        <div className="navbar-actions">
          <button
            type="button"
            className="theme-toggle"
            onClick={toggleTheme}
            aria-label={`Switch to ${theme === "dark" ? "light" : "dark"} theme`}
            title={`Switch to ${theme === "dark" ? "light" : "dark"} theme`}
          >
            <Sun size={16} weight={theme === "dark" ? "fill" : "regular"} />
            <Moon size={16} weight={theme === "light" ? "fill" : "regular"} />
          </button>
          <Link className="navbar-action" href="/profile" aria-label="Open profile">
            <UserCircle size={19} />
          </Link>
          <WalletConnectButton />
          <button
            className="ui-icon-button navbar-menu-button"
            onClick={() => setMenuOpen(true)}
            aria-label="Open navigation"
          >
            <List size={22} />
          </button>
        </div>
      </header>
      <main className="app-content">
        {children}
        <footer className="app-footer" aria-label="Site footer">
          <div className="footer-brand">
            <span className="footer-name">Launchpad</span>
            <p>
              Launch fixed-supply tokens on Robinhood Chain. Your selected wallet signs every
              transaction; Launchpad never takes custody.
            </p>
          </div>
          <nav className="footer-navigation" aria-label="Footer navigation">
            <span>Product</span>
            <Link href="/">Explore</Link>
            <Link href="/create">Create</Link>
            <Link href="/analytics">Analytics</Link>
            <Link href="/profile">Profile</Link>
            <Link href="/docs">Docs</Link>
          </nav>
          <div className="footer-risk">
            <span>Risk notice</span>
            <p>
              On-chain transactions may be irreversible. Tokens can be volatile or lose all value.
            </p>
            <div className="footer-links">
              <Link href="/docs#risk">Risk and finality</Link>
              {configuration.deployment?.explorerBase ? (
                <SafeExternalLink href={configuration.deployment.explorerBase}>
                  Explorer <ArrowSquareOut size={13} />
                </SafeExternalLink>
              ) : (
                <span className="ui-disabled-link">Explorer unavailable</span>
              )}
            </div>
          </div>
          <p className="footer-meta">© 2026 Launchpad</p>
        </footer>
      </main>
      <Sheet open={menuOpen} title="Navigate" onClose={() => setMenuOpen(false)}>
        <nav className="sheet-nav" aria-label="Mobile menu navigation">
          {navItems.map(({ href, label, icon: Icon }) => (
            <Link
              key={href}
              href={href}
              onClick={() => setMenuOpen(false)}
              className={active(href) ? "is-active" : ""}
            >
              <Icon size={20} />
              {label}
            </Link>
          ))}
        </nav>
        <div className="sheet-note">
          <ShieldCheck size={18} />
          <p>
            Wallet connections and signing remain user-controlled. Launchpad never takes custody of
            funds.
          </p>
        </div>
      </Sheet>
    </div>
  );
}
