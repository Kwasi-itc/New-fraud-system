import type { ComponentProps } from "react";

import { cva, type VariantProps } from "class-variance-authority";

import { cn } from "@/lib/utils";

const badgeVariants = cva(
  "inline-flex items-center rounded-full border px-3 py-1 text-xs font-semibold tracking-[0.18em] uppercase",
  {
    variants: {
      variant: {
        neutral: "border-border bg-muted text-foreground",
        accent: "border-blue-200 bg-blue-50 text-blue-700",
        success: "border-green-200 bg-green-50 text-green-700",
        warning: "border-amber-200 bg-amber-50 text-amber-700",
      },
    },
    defaultVariants: {
      variant: "neutral",
    },
  }
);

export function Badge({
  className,
  variant,
  ...props
}: ComponentProps<"div"> & VariantProps<typeof badgeVariants>) {
  return (
    <div className={cn(badgeVariants({ variant }), className)} {...props} />
  );
}
