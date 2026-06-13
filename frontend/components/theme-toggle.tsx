"use client";

import { Moon, Sun } from "lucide-react";

import { Button } from "@/components/ui/button";
import { useTheme } from "@/lib/use-theme";

export function ThemeToggle({ collapsed = false }: { collapsed?: boolean }) {
  const { theme, toggle } = useTheme();
  const label = theme === "dark" ? "Switch to light mode" : "Switch to dark mode";

  return (
    <Button
      variant="ghost"
      onClick={toggle}
      title={label}
      aria-label={label}
      className={`w-full gap-3 ${collapsed ? "justify-center px-0" : "justify-start"}`}
    >
      {theme === "dark" ? <Sun className="size-5 shrink-0" /> : <Moon className="size-5 shrink-0" />}
      {!collapsed && <span className="truncate">{theme === "dark" ? "Light mode" : "Dark mode"}</span>}
    </Button>
  );
}
