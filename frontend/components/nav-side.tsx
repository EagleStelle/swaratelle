"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { PanelLeftClose, PanelLeftOpen } from "lucide-react";

import { LogoMark } from "@/components/logo-mark";
import { NAV, isActive } from "@/components/nav-items";
import { ThemeToggle } from "@/components/theme-toggle";
import { Button } from "@/components/ui/button";
import { useSidebarStore } from "@/lib/ui-store";

export function NavSide() {
  const pathname = usePathname();
  const collapsed = useSidebarStore((s) => s.collapsed);
  const toggle = useSidebarStore((s) => s.toggle);

  return (
    <aside
      data-collapsed={collapsed}
      className={`sticky top-0 hidden h-screen shrink-0 flex-col border-r border-sidebar-border bg-sidebar text-sidebar-foreground md:flex ${
        collapsed ? "w-16" : "w-60"
      }`}
    >
      <div
        className={`flex h-14 shrink-0 items-center px-3 ${
          collapsed ? "justify-center" : "justify-between gap-2"
        }`}
      >
        {collapsed ? (
          <div className="group relative size-9">
            <Link
              href="/"
              title="Swaratelle"
              aria-label="Swaratelle home"
              className="absolute inset-0 flex items-center justify-center rounded-md text-sidebar-foreground transition-opacity group-hover:opacity-0"
            >
              <LogoMark className="size-7" />
            </Link>
            <Button
              variant="ghost"
              size="icon"
              onClick={toggle}
              aria-label="Expand sidebar"
              className="absolute inset-0 opacity-0 transition-opacity group-hover:opacity-100"
            >
              <PanelLeftOpen className="size-5" />
            </Button>
          </div>
        ) : (
          <>
            <Link
              href="/"
              title="Swaratelle"
              aria-label="Swaratelle home"
              className="flex min-w-0 items-center gap-2 rounded-md px-1 py-1 text-sidebar-foreground transition-colors hover:text-sidebar-accent-foreground"
            >
              <LogoMark className="size-8" />
              <span className="truncate text-sm font-semibold">Swaratelle</span>
            </Link>
            <Button
              variant="ghost"
              size="icon"
              onClick={toggle}
              aria-label="Collapse sidebar"
            >
              <PanelLeftClose className="size-5" />
            </Button>
          </>
        )}
      </div>

      <nav className="flex flex-1 flex-col gap-1 px-2">
        {NAV.map(({ href, label, icon: Icon }) => (
          <Button
            key={href}
            asChild
            variant="ghost"
            className={`w-full gap-3 aria-[current=page]:bg-sidebar-accent aria-[current=page]:font-medium aria-[current=page]:text-sidebar-accent-foreground ${
              collapsed ? "justify-center px-0" : "justify-start"
            }`}
          >
            <Link
              href={href}
              title={collapsed ? label : undefined}
              aria-current={isActive(pathname, href) ? "page" : undefined}
            >
              <Icon className="size-5 shrink-0" />
              {!collapsed && <span className="truncate">{label}</span>}
            </Link>
          </Button>
        ))}
      </nav>

      <div className="border-t border-sidebar-border p-2">
        <ThemeToggle collapsed={collapsed} />
      </div>
    </aside>
  );
}
