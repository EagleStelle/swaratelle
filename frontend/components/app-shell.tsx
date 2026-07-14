"use client";

import type { ReactNode } from "react";

import { NavSide } from "@/components/nav-side";
import { NavBottom } from "@/components/nav-bottom";

/** The authenticated app chrome: side nav (desktop), content, bottom nav (mobile). */
export function AppShell({ children }: { children: ReactNode }) {
  return (
    <>
      <div className="flex min-h-screen">
        <NavSide />
        <main className="flex min-w-0 flex-1 flex-col bg-slate-100 p-4 pb-0 dark:bg-background">
          {children}
        </main>
      </div>
      <NavBottom />
    </>
  );
}
