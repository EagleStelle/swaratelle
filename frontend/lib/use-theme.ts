"use client";

import { useEffect, useState } from "react";

const STORAGE_KEY = "swaratelle-theme";

/**
 * Reads and toggles the color scheme. The class is set before first paint by
 * the inline script in the root layout; this hook mirrors and updates it.
 */
export function useTheme() {
  const [theme, setTheme] = useState<"light" | "dark">("light");

  useEffect(() => {
    setTheme(
      document.documentElement.classList.contains("dark") ? "dark" : "light"
    );
  }, []);

  function toggle() {
    const next = theme === "dark" ? "light" : "dark";
    setTheme(next);
    document.documentElement.classList.toggle("dark", next === "dark");
    localStorage.setItem(STORAGE_KEY, next);
  }

  return { theme, toggle };
}
