import type { ComponentProps } from "react";

import { cn } from "@/lib/utils";

export function Card({
  className,
  ...props
}: ComponentProps<"div">) {
  return (
    <div
      data-slot="card"
      className={cn(
        "rounded-[28px] border border-border/80 bg-card text-card-foreground shadow-[0_18px_55px_rgba(15,23,42,0.06)]",
        className
      )}
      {...props}
    />
  );
}

export function CardHeader({
  className,
  ...props
}: ComponentProps<"div">) {
  return (
    <div
      data-slot="card-header"
      className={cn("flex flex-col gap-2 p-6", className)}
      {...props}
    />
  );
}

export function CardTitle({
  className,
  ...props
}: ComponentProps<"h3">) {
  return (
    <h3
      data-slot="card-title"
      className={cn("text-lg font-semibold tracking-tight", className)}
      {...props}
    />
  );
}

export function CardDescription({
  className,
  ...props
}: ComponentProps<"p">) {
  return (
    <p
      data-slot="card-description"
      className={cn("text-sm leading-6 text-muted-foreground", className)}
      {...props}
    />
  );
}

export function CardContent({
  className,
  ...props
}: ComponentProps<"div">) {
  return (
    <div data-slot="card-content" className={cn("p-6 pt-0", className)} {...props} />
  );
}
