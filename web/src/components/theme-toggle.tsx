"use client";

import { Moon, Sun } from "./icons";
import { originOf, setThemePreference, useResolvedTheme } from "./theme";

/** One header button: each press flips between the dark and light palettes and remembers it. */
export function ThemeToggle() {
  const theme = useResolvedTheme();
  const next = theme === "dark" ? "light" : "dark";
  const label = `Switch to ${next} theme`;
  return (
    <button
      type="button"
      className="ui-icon-button navbar-theme"
      aria-label={label}
      title={label}
      onClick={(event) => setThemePreference(next, originOf(event.currentTarget))}
    >
      {theme === "dark" ? (
        <Sun size={18} aria-hidden="true" />
      ) : (
        <Moon size={18} aria-hidden="true" />
      )}
    </button>
  );
}
