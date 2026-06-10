import Image from "next/image";

import { cn } from "@/lib/utils";

type FdsLogoProps = {
  className?: string;
  compact?: boolean;
  stacked?: boolean;
  title?: string;
  subtitle?: string;
  logoSrc?: string;
  logoAlt?: string;
  hideText?: boolean;
};

export function FdsLogo({
  className,
  compact = false,
  stacked = false,
  title = "FRAUD DESK",
  subtitle = "Detection Control Center",
  logoSrc,
  logoAlt = "FDs logo",
  hideText = false,
}: FdsLogoProps) {
  return (
    <div
      className={cn(
        "flex items-center gap-3",
        stacked && "flex-col gap-4 text-center",
        className
      )}
    >
      {logoSrc ? (
        <div
          className={cn(
            "relative h-12 w-[220px]",
            compact && "h-10 w-[180px]",
            stacked && "h-16 w-[280px] sm:h-20 sm:w-[340px]"
          )}
        >
          <Image
            src={logoSrc}
            alt={logoAlt}
            fill
            sizes={stacked ? "(max-width: 640px) 280px, 340px" : compact ? "180px" : "220px"}
            className="object-contain"
            priority={stacked}
          />
        </div>
      ) : (
        <div className="flex size-10 items-center justify-center rounded-2xl bg-accent text-sm font-semibold text-accent-foreground shadow-[0_10px_30px_rgba(37,99,235,0.28)]">
          FDs
        </div>
      )}
      {!compact && !hideText ? (
        <div className={cn("flex flex-col", stacked && "items-center")}>
          <span
            className={cn(
              "text-sm font-semibold tracking-[0.24em] text-foreground",
              stacked && "text-[2.65rem] leading-none tracking-tight sm:text-5xl"
            )}
          >
            {title}
          </span>
          <span
            className={cn(
              "text-xs text-muted-foreground",
              stacked && "mt-1 text-base font-normal text-slate-600 sm:text-lg"
            )}
          >
            {subtitle}
          </span>
        </div>
      ) : null}
    </div>
  );
}
