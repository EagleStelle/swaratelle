"use client";

import { useState } from "react";
import {
  ChevronUp,
  CircleUserRound,
  LogOut,
  Moon,
  Sun,
  UserCog,
} from "lucide-react";

import { AccountModal } from "@/components/auth/account-modal";
import { Button } from "@/components/ui/button";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { useAuthSession, useLogout } from "@/lib/use-auth";
import { useTheme } from "@/lib/use-theme";
import { cn } from "@/lib/utils";

type AccountMenuVariant = "sidebar" | "sidebar-collapsed" | "bottom";

const menuItemClass =
  "flex w-full items-center gap-2.5 rounded-md px-2 py-2 text-sm font-medium hover:bg-accent hover:text-accent-foreground";

export function AccountMenu({ variant }: { variant: AccountMenuVariant }) {
  const session = useAuthSession();
  const logout = useLogout();
  const { theme, toggle } = useTheme();
  const [open, setOpen] = useState(false);
  const [modalOpen, setModalOpen] = useState(false);

  const username = session.data?.username || "root";
  const collapsed = variant === "sidebar-collapsed";
  const bottom = variant === "bottom";

  // Popover placement per nav position: above the sidebar footer, to the right
  // of the collapsed rail, above the bottom bar.
  const side = collapsed ? "right" : "top";
  const align = bottom ? "end" : collapsed ? "end" : "start";

  function openAccount() {
    setOpen(false);
    setModalOpen(true);
  }

  function chooseTheme() {
    toggle();
    setOpen(false);
  }

  function chooseLogout() {
    setOpen(false);
    logout.mutate();
  }

  return (
    <>
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger asChild>
          {bottom ? (
            <Button
              variant="ghost"
              aria-label="Account"
              className="flex h-full flex-1 flex-col items-center justify-center gap-1 rounded-none text-xs font-normal text-muted-foreground hover:text-foreground data-[state=open]:text-foreground"
            >
              <CircleUserRound className="size-5" />
              <span>Account</span>
            </Button>
          ) : collapsed ? (
            <Button
              variant="ghost"
              size="icon"
              aria-label="Account"
              title={username}
            >
              <CircleUserRound className="size-5" />
            </Button>
          ) : (
            <Button
              variant="ghost"
              aria-label="Account"
              className="h-auto w-full justify-start gap-3 py-2"
            >
              <CircleUserRound className="size-5 shrink-0" />
              <span className="flex min-w-0 flex-1 flex-col text-left">
                <span className="truncate text-sm font-medium leading-tight">
                  {username}
                </span>
                <span className="truncate text-xs leading-tight text-muted-foreground">
                  Root account
                </span>
              </span>
              <ChevronUp
                className={cn(
                  "size-4 shrink-0 text-muted-foreground transition-transform",
                  open && "rotate-180",
                )}
              />
            </Button>
          )}
        </PopoverTrigger>

        <PopoverContent
          side={side}
          align={align}
          className="w-56 p-1"
          role="menu"
          aria-label="Account menu"
        >
          <div className="truncate px-2 py-1.5 text-sm font-medium text-muted-foreground">
            {username}
          </div>
          <button type="button" className={menuItemClass} onClick={chooseTheme}>
            {theme === "dark" ? (
              <Sun className="size-4 shrink-0" />
            ) : (
              <Moon className="size-4 shrink-0" />
            )}
            <span>{theme === "dark" ? "Light mode" : "Dark mode"}</span>
          </button>
          <button type="button" className={menuItemClass} onClick={openAccount}>
            <UserCog className="size-4 shrink-0" />
            <span>Account</span>
          </button>
          <div className="my-1 h-px bg-border" />
          <button
            type="button"
            className={cn(menuItemClass, "text-destructive hover:text-destructive")}
            onClick={chooseLogout}
          >
            <LogOut className="size-4 shrink-0" />
            <span>Logout</span>
          </button>
        </PopoverContent>
      </Popover>

      <AccountModal
        open={modalOpen}
        onOpenChange={setModalOpen}
        username={username}
      />
    </>
  );
}
