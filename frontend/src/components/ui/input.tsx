import type { ComponentProps } from "react";

import { cn } from "@/lib/utils";

export function Input({ className, ...props }: ComponentProps<"input">) {
  return (
    <input
      data-slot="input"
      className={cn(
        "flex h-12 w-full rounded-2xl border border-input bg-white px-4 text-sm text-foreground outline-none placeholder:text-muted-foreground focus:border-foreground focus:ring-4 focus:ring-black/5",
        className
      )}
      {...props}
    />
  );
}
