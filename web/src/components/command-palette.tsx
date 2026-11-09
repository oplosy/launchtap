"use client";

import { useRouter } from "next/navigation";
import { useEffect, useId, useMemo, useRef, useState } from "react";
import {
  ArrowRight,
  BookOpenText,
  ChartLineUp,
  Compass,
  Desktop,
  MagnifyingGlass,
  Moon,
  Plus,
  Sun,
  UserCircle,
} from "@phosphor-icons/react";
import { ApiClient } from "@/api/client";
import type { components } from "@/api/generated";
import { publicConfiguration } from "@/config/public";
import { shortAddress } from "@/token/address";
import { setThemePreference, type ThemePreference } from "./theme";

type Token = components["schemas"]["TokenDTO"];
type Command = {
  id: string;
  group: "Tokens" | "Go to" | "Theme";
  label: string;
  hint?: string;
  keywords?: string;
  icon: React.ReactNode;
  run: () => void;
};

/** The API rejects search text longer than this many UTF-8 bytes. */
const MAX_QUERY_BYTES = 64;

const PAGES = [
  { href: "/", label: "Explore", keywords: "home launches tokens", icon: <Compass /> },
  { href: "/create", label: "Create a launch", keywords: "new token launch", icon: <Plus /> },
  { href: "/analytics", label: "Analytics", keywords: "stats volume", icon: <ChartLineUp /> },
  { href: "/docs", label: "Docs", keywords: "help curve fees risk", icon: <BookOpenText /> },
  { href: "/profile", label: "Profile", keywords: "account wallet claims", icon: <UserCircle /> },
] as const;

const THEMES: ReadonlyArray<{ value: ThemePreference; label: string; icon: React.ReactNode }> = [
  { value: "system", label: "Use system theme", icon: <Desktop /> },
  { value: "light", label: "Use light theme", icon: <Sun /> },
  { value: "dark", label: "Use dark theme", icon: <Moon /> },
];

export default function CommandPalette({ open, onClose }: { open: boolean; onClose: () => void }) {
  const router = useRouter();
  const configuration = publicConfiguration();
  const ref = useRef<HTMLDialogElement>(null);
  const listId = useId();
  const [query, setQuery] = useState("");
  const [active, setActive] = useState(0);
  const [tokens, setTokens] = useState<Token[]>([]);
  const [searching, setSearching] = useState(false);

  useEffect(() => {
    const dialog = ref.current;
    if (!dialog) return;
    if (open && !dialog.open) dialog.showModal();
    if (!open && dialog.open) dialog.close();
  }, [open]);

  const term = query.trim();
  const searchable =
    configuration.status === "ready" &&
    Boolean(configuration.apiBaseUrl) &&
    term.length > 0 &&
    new TextEncoder().encode(term).length <= MAX_QUERY_BYTES;

  useEffect(() => {
    if (!searchable || !configuration.apiBaseUrl) return;
    const controller = new AbortController();
    const client = new ApiClient({ baseUrl: configuration.apiBaseUrl });
    const timer = window.setTimeout(() => {
      setSearching(true);
      // The list endpoint is scoped to one phase, so both are asked and merged.
      void Promise.all(
        (["graduated", "curve"] as const).map((phase) =>
          client.getTokens({ phase, q: term, limit: 4 }, controller.signal),
        ),
      )
        .then((pages) => setTokens(pages.flatMap((page) => page.items ?? []).slice(0, 6)))
        .catch(() => {
          if (!controller.signal.aborted) setTokens([]);
        })
        .finally(() => {
          if (!controller.signal.aborted) setSearching(false);
        });
    }, 180);
    return () => {
      window.clearTimeout(timer);
      controller.abort();
    };
  }, [configuration.apiBaseUrl, searchable, term]);

  const commands = useMemo<Command[]>(() => {
    const close = (then: () => void) => () => {
      onClose();
      then();
    };
    const matches = (text: string) => text.toLowerCase().includes(term.toLowerCase());
    const tokenCommands: Command[] = searchable
      ? tokens.map((token) => ({
          id: `token-${token.address}`,
          group: "Tokens",
          label: token.name.trim() || token.symbol.trim() || "Unnamed token",
          hint: `${token.symbol || "—"} · ${shortAddress(token.address)}`,
          icon: <span className="command-token">{(token.symbol || token.name).charAt(0)}</span>,
          run: close(() => router.push(`/token/${token.address}` as never)),
        }))
      : [];
    const pageCommands: Command[] = PAGES.filter(
      (page) => !term || matches(`${page.label} ${page.keywords}`),
    ).map((page) => ({
      id: `page-${page.href}`,
      group: "Go to",
      label: page.label,
      icon: page.icon,
      run: close(() => router.push(page.href as never)),
    }));
    const themeCommands: Command[] = THEMES.filter(
      (theme) => !term || matches(`${theme.label} theme appearance`),
    ).map((theme) => ({
      id: `theme-${theme.value}`,
      group: "Theme",
      label: theme.label,
      icon: theme.icon,
      run: close(() => setThemePreference(theme.value)),
    }));
    return [...tokenCommands, ...pageCommands, ...themeCommands];
  }, [onClose, router, searchable, term, tokens]);

  const current = Math.min(active, Math.max(0, commands.length - 1));
  const groups = commands.reduce<Array<{ name: Command["group"]; items: Command[] }>>(
    (result, command) => {
      const last = result.at(-1);
      if (last?.name === command.group) last.items.push(command);
      else result.push({ name: command.group, items: [command] });
      return result;
    },
    [],
  );

  return (
    <dialog
      ref={ref}
      className="ui-dialog command-palette"
      aria-label="Command menu"
      onCancel={(event) => {
        event.preventDefault();
        onClose();
      }}
      onClose={onClose}
      onClick={(event) => {
        if (event.target === event.currentTarget) onClose();
      }}
    >
      <div className="command-search">
        <MagnifyingGlass size={18} aria-hidden="true" />
        <input
          autoFocus
          role="combobox"
          aria-expanded="true"
          aria-controls={listId}
          aria-activedescendant={commands[current] ? `${listId}-${current}` : undefined}
          aria-label="Search tokens, pages and settings"
          placeholder="Search tokens, pages, settings…"
          value={query}
          maxLength={MAX_QUERY_BYTES}
          autoComplete="off"
          spellCheck={false}
          onChange={(event) => {
            setQuery(event.target.value);
            setActive(0);
          }}
          onKeyDown={(event) => {
            if (event.key === "ArrowDown" || event.key === "ArrowUp") {
              event.preventDefault();
              const step = event.key === "ArrowDown" ? 1 : -1;
              setActive((current + step + commands.length) % Math.max(commands.length, 1));
            } else if (event.key === "Enter") {
              event.preventDefault();
              commands[current]?.run();
            }
          }}
        />
        <kbd>Esc</kbd>
      </div>
      <div className="command-list" id={listId} role="listbox" aria-label="Results">
        {groups.map((group) => (
          <div className="command-group" key={group.name} role="presentation">
            <p className="command-group-label" role="presentation">
              {group.name}
            </p>
            {group.items.map((command) => {
              const index = commands.indexOf(command);
              return (
                <div
                  key={command.id}
                  id={`${listId}-${index}`}
                  role="option"
                  aria-selected={index === current}
                  className="command-item"
                  onPointerMove={() => setActive(index)}
                  onClick={command.run}
                >
                  <span className="command-icon" aria-hidden="true">
                    {command.icon}
                  </span>
                  <span className="command-label">{command.label}</span>
                  {command.hint ? <span className="command-hint mono">{command.hint}</span> : null}
                  <ArrowRight className="command-go" size={14} aria-hidden="true" />
                </div>
              );
            })}
          </div>
        ))}
        {searchable && searching ? <p className="command-empty">Searching tokens…</p> : null}
        {!(searchable && searching) && commands.length === 0 ? (
          <p className="command-empty">Nothing matches “{term}”.</p>
        ) : null}
      </div>
      <div className="command-foot" aria-hidden="true">
        <span>
          <kbd>↑</kbd>
          <kbd>↓</kbd> to move
        </span>
        <span>
          <kbd>↵</kbd> to open
        </span>
      </div>
    </dialog>
  );
}
