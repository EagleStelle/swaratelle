"use client";

import type { ReactNode } from "react";

import { AppShell } from "@/components/app-shell";
import { Login } from "@/components/auth/login";
import { LogoMark } from "@/components/logo-mark";
import { useAuthSession } from "@/lib/use-auth";

/**
 * Gates the whole app on the UI login. While the session check is in flight it
 * shows a splash; unauthenticated visitors get the login screen; everyone else
 * gets the app shell. External API clients bypass all of this via the bearer
 * token and never load the UI.
 */
export function AuthGate({ children }: { children: ReactNode }) {
  const session = useAuthSession();

  if (session.isPending) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-slate-100 dark:bg-background">
        <LogoMark className="size-10 animate-pulse" />
      </div>
    );
  }

  if (!session.data?.authenticated) {
    return <Login />;
  }

  return <AppShell>{children}</AppShell>;
}
