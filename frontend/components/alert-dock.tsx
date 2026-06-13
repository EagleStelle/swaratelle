import * as React from "react";

import { cn } from "@/lib/utils";

type AlertDockProps = React.PropsWithChildren<{
  className?: string;
  elevated?: boolean;
}>;

export function AlertDock({ children, className, elevated }: AlertDockProps) {
  const items = React.Children.toArray(children);
  if (items.length === 0) return null;

  return (
    <div
      className={cn(
        "pointer-events-none fixed inset-x-4 bottom-20 z-40 flex flex-col gap-2 md:inset-x-auto md:bottom-auto md:right-4 md:top-4 md:w-full md:max-w-md",
        elevated && "bottom-36 md:top-16",
        className
      )}
    >
      {items.map((child, index) => (
        <div key={index} className="pointer-events-auto">
          {child}
        </div>
      ))}
    </div>
  );
}
