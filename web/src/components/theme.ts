"use client";

import { useSyncExternalStore } from "react";
import { THEME_COOKIE } from "./theme-cookie";

export type ThemePreference = "system" | "light" | "dark";

const listeners = new Set<() => void>();

export function readThemePreference(): ThemePreference {
  const value = document.documentElement.dataset.theme;
  return value === "light" || value === "dark" ? value : "system";
}

function systemTheme(): "light" | "dark" {
  return window.matchMedia("(prefers-color-scheme: light)").matches ? "light" : "dark";
}

export function resolvedTheme(preference = readThemePreference()): "light" | "dark" {
  return preference === "system" ? systemTheme() : preference;
}

function subscribe(listener: () => void) {
  const media = window.matchMedia("(prefers-color-scheme: light)");
  listeners.add(listener);
  media.addEventListener("change", listener);
  return () => {
    listeners.delete(listener);
    media.removeEventListener("change", listener);
  };
}

function applyPreference(next: ThemePreference) {
  const root = document.documentElement;
  if (next === "system") delete root.dataset.theme;
  else root.dataset.theme = next;
  const secure = window.location.protocol === "https:" ? "; secure" : "";
  document.cookie =
    next === "system"
      ? `${THEME_COOKIE}=; path=/; max-age=0; samesite=lax${secure}`
      : `${THEME_COOKIE}=${next}; path=/; max-age=31536000; samesite=lax${secure}`;
  for (const listener of listeners) listener();
}

/**
 * Persists the choice and, where the browser supports view transitions, reveals the new palette
 * as a circle growing from `origin` (usually the control that was pressed).
 */
export function setThemePreference(next: ThemePreference, origin?: { x: number; y: number }) {
  const changesPalette = resolvedTheme() !== resolvedTheme(next);
  const reduce = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
  if (!changesPalette || reduce || !("startViewTransition" in document)) {
    applyPreference(next);
    return;
  }
  const x = origin?.x ?? window.innerWidth / 2;
  const y = origin?.y ?? 0;
  const radius = Math.hypot(
    Math.max(x, window.innerWidth - x),
    Math.max(y, window.innerHeight - y),
  );
  const transition = document.startViewTransition(() => applyPreference(next));
  transition.ready
    .then(() => {
      document.documentElement.animate(
        { clipPath: [`circle(0px at ${x}px ${y}px)`, `circle(${radius}px at ${x}px ${y}px)`] },
        {
          duration: 620,
          easing: "cubic-bezier(0.22, 1, 0.36, 1)",
          pseudoElement: "::view-transition-new(root)",
        },
      );
    })
    .catch(() => undefined);
}

export function useThemePreference(): ThemePreference {
  return useSyncExternalStore(subscribe, readThemePreference, () => "system");
}

export function useResolvedTheme(): "light" | "dark" {
  return useSyncExternalStore(
    subscribe,
    () => resolvedTheme(),
    () => "dark",
  );
}

export function originOf(element: Element) {
  const box = element.getBoundingClientRect();
  return { x: box.left + box.width / 2, y: box.top + box.height / 2 };
}
