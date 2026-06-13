import { Download, History, type LucideIcon } from "lucide-react";

export interface NavItem {
  href: string;
  label: string;
  icon: LucideIcon;
}

// Downloads = where you add URLs and watch active fetches; History = finished.
export const NAV: NavItem[] = [
  { href: "/", label: "Downloads", icon: Download },
  { href: "/history", label: "History", icon: History },
];

// Static export hosts can serve trailing-slash paths (e.g. "/history/"), so a raw
// `pathname === href` comparison drops the active state after client nav.
export function isActive(pathname: string | null, href: string): boolean {
  if (!pathname) return false;
  const norm = (p: string) => (p.length > 1 ? p.replace(/\/+$/, "") : p);
  return norm(pathname) === norm(href);
}
