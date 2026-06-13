import { cn } from "@/lib/utils";

export function LogoMark({ className }: { className?: string }) {
  return (
    <span
      aria-hidden="true"
      className={cn("logo-mask size-7 shrink-0", className)}
    />
  );
}
