"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { Moon, Sun } from "lucide-react";

import { NAV, isActive } from "@/components/nav-items";
import { Button } from "@/components/ui/button";
import { useTheme } from "@/lib/use-theme";

const itemClass =
  "flex h-full flex-1 flex-col items-center justify-center gap-1 rounded-none text-xs font-normal text-muted-foreground hover:text-foreground";

export function NavBottom() {
  const pathname = usePathname();
  const { theme, toggle } = useTheme();

  return (
    <nav className="fixed inset-x-0 bottom-0 z-50 flex h-16 items-stretch border-t border-sidebar-border bg-sidebar text-sidebar-foreground md:hidden">
      {NAV.map(({ href, label, icon: Icon }) => (
        <Button
          key={href}
          asChild
          variant="ghost"
          className={`${itemClass} aria-[current=page]:text-foreground`}
        >
          <Link
            href={href}
            aria-current={isActive(pathname, href) ? "page" : undefined}
          >
            <Icon className="size-5" />
            <span>{label}</span>
          </Link>
        </Button>
      ))}
      <Button
        variant="ghost"
        onClick={toggle}
        aria-label={theme === "dark" ? "Switch to light mode" : "Switch to dark mode"}
        className={itemClass}
      >
        {theme === "dark" ? <Sun className="size-5" /> : <Moon className="size-5" />}
        <span>Theme</span>
      </Button>
    </nav>
  );
}
