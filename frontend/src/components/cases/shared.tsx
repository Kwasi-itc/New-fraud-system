"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import type { ReactNode } from "react";
import {
  AlertCircle,
  Briefcase,
  BarChart3,
  Inbox,
  Tag,
  Settings,
  RotateCcw,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

export const fieldClass =
  "w-full rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm text-slate-900 placeholder:text-slate-400 focus:border-blue-500 focus:outline-hidden focus:ring-1 focus:ring-blue-500 transition-colors";
export const labelClass = "grid gap-1.5 text-sm font-semibold uppercase tracking-wider text-slate-600";

export function ClosingFindings({ label }: { label: string }) {
  return (
    <label className={labelClass}>
      <span>{label}</span>
      <textarea
        aria-label={label}
        name="comment"
        required
        rows={3}
        className={cn(fieldClass, "font-normal normal-case")}
        placeholder="Document closing rationale and findings..."
        onInput={(event) => {
          const field = event.currentTarget;
          field.setCustomValidity(
            !field.value.trim()
              ? "Enter closing findings, not just spaces."
              : new TextEncoder().encode(field.value).length > 10000
              ? "Closing findings must be at most 10,000 bytes. Shorten the text."
              : ""
          );
        }}
      />
      <span className="text-sm font-normal normal-case text-slate-500">
        Required. Maximum 10,000 bytes; some characters use more than one byte.
      </span>
    </label>
  );
}

export function Panel({
  title,
  children,
  action,
  className,
}: {
  title: string;
  children: ReactNode;
  action?: ReactNode;
  className?: string;
}) {
  return (
    <section
      className={cn(
        "rounded-xl border border-slate-200/90 bg-white p-5 shadow-xs transition-all",
        className
      )}
    >
      <div className="flex items-center justify-between gap-3 mb-4 pb-3 border-b border-slate-100">
        <h2 className="text-base font-semibold tracking-tight text-slate-900">{title}</h2>
        {action && <div>{action}</div>}
      </div>
      <div className="space-y-4">{children}</div>
    </section>
  );
}

export function WorkspaceTabs<T extends string>({
  tabs,
  activeTab,
  onChange,
}: {
  tabs: { id: T; label: string; count?: number; icon?: React.ComponentType<{ className?: string }> }[];
  activeTab: T;
  onChange: (tabId: T) => void;
}) {
  return (
    <div
      role="tablist"
      aria-label="Investigation sections"
      className="flex items-center gap-1.5 overflow-x-auto border-b border-slate-200 bg-white px-2 py-1.5 text-sm font-medium rounded-xl shadow-2xs mb-6"
    >
      {tabs.map((tab) => {
        const Icon = tab.icon;
        const isActive = activeTab === tab.id;
        return (
          <button
            key={tab.id}
            role="tab"
            aria-selected={isActive}
            aria-controls={`panel-${tab.id}`}
            id={`tab-${tab.id}`}
            type="button"
            onClick={() => onChange(tab.id)}
            className={cn(
              "inline-flex shrink-0 items-center gap-2 rounded-lg px-3 py-2 text-sm font-semibold transition-all cursor-pointer",
              isActive
                ? "bg-slate-900 text-white shadow-2xs"
                : "text-slate-600 hover:bg-slate-100 hover:text-slate-900"
            )}
          >
            {Icon && <Icon className={cn("size-4", isActive ? "text-slate-300" : "text-slate-400")} />}
            <span>{tab.label}</span>
            {typeof tab.count === "number" && (
              <span
                className={cn(
                  "rounded-full px-1.5 py-0.2 font-mono text-xs font-semibold",
                  isActive
                    ? "bg-slate-800 text-slate-200"
                    : "bg-slate-100 text-slate-600"
                )}
              >
                {tab.count}
              </span>
            )}
          </button>
        );
      })}
    </div>
  );
}

export function Problem({
  error,
  retry,
}: {
  error: Error | null;
  retry?: () => void;
}) {
  if (!error) return null;
  return (
    <div
      role="alert"
      className="flex items-start justify-between gap-3 rounded-lg border border-rose-200 bg-rose-50/80 p-3.5 text-sm text-rose-900"
    >
      <div className="flex items-start gap-2.5 min-w-0">
        <AlertCircle className="size-4 shrink-0 text-rose-600 mt-0.5" />
        <span className="font-medium break-words">{error.message}</span>
      </div>
      {retry && (
        <Button
          size="sm"
          variant="outline"
          onClick={retry}
          className="shrink-0 h-8 text-sm border-rose-300 text-rose-800 hover:bg-rose-100/60 gap-1.5"
        >
          <RotateCcw className="size-3.5" />
          Retry
        </Button>
      )}
    </div>
  );
}

export function CaseNav({
  tenant,
  queue,
}: {
  tenant: string;
  queue?: string;
}) {
  const pathname = usePathname();

  const links = [
    {
      name: "Case queue",
      href: `/cases?${new URLSearchParams({ tenant, ...(queue !== undefined ? { queue } : {}) })}`,
      active: pathname === "/cases",
      icon: Briefcase,
    },
    {
      name: "Analytics",
      href: `/cases/analytics?tenant=${tenant}`,
      active: pathname?.startsWith("/cases/analytics"),
      icon: BarChart3,
    },
    {
      name: "Settings",
      href: `/cases/settings?tenant=${tenant}`,
      active:
        pathname?.startsWith("/cases/settings") ||
        pathname?.startsWith("/cases/inboxes") ||
        pathname?.startsWith("/cases/tags"),
      icon: Settings,
    },
  ];

  return (
    <nav className="flex items-center gap-2 border-b border-slate-200/90 pb-3 text-sm font-medium">
      {links.map((link) => {
        const Icon = link.icon;
        return (
          <Link
            key={link.name}
            href={link.href}
            className={cn(
              "inline-flex items-center gap-2 rounded-lg px-3.5 py-2 text-sm font-semibold transition-all",
              link.active
                ? "bg-blue-50 text-blue-700 shadow-2xs"
                : "text-slate-600 hover:bg-slate-100 hover:text-slate-900"
            )}
          >
            <Icon className={cn("size-4", link.active ? "text-blue-600" : "text-slate-400")} />
            {link.name}
          </Link>
        );
      })}
    </nav>
  );
}

export function readable(value?: string) {
  return value?.replaceAll("_", " ") ?? "—";
}

export function timestamp(value: string) {
  if (!value) return "—";
  const date = new Date(value);
  if (isNaN(date.getTime())) return value;
  return date.toLocaleString();
}
