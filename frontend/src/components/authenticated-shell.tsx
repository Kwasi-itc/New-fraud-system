"use client";

import { useState, type ComponentType, type ReactNode } from "react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import {
  ChevronLeft,
  ChevronRight,
  Database,
  Shield,
  Settings,
  SquareUserRound,
  FolderSearch2,
  UserCircle2,
} from "lucide-react";

import { FdsLogo } from "@/components/fds-logo";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

type AuthenticatedShellProps = {
  children: ReactNode;
};

const primaryLinks = [
  {
    href: "/detection",
    label: "Detection",
    icon: Shield,
  },
  {
    href: "/cases",
    label: "Case Manager",
    icon: FolderSearch2,
  },
  {
    href: "/investigator",
    label: "Customer hub",
    icon: SquareUserRound,
  },
];

const secondaryLinks = [
  {
    href: "/your-data",
    label: "Your Data Model",
    icon: Database,
  },
  {
    href: "/settings",
    label: "Settings",
    icon: Settings,
  },
  {
    href: "/my-account",
    label: "My Account",
    icon: UserCircle2,
  },
];

function NavLink({
  href,
  label,
  icon: Icon,
  collapsed,
}: {
  href: string;
  label: string;
  icon: ComponentType<{ className?: string }>;
  collapsed: boolean;
}) {
  const pathname = usePathname();
  const isActive =
    pathname === href || pathname.startsWith(`${href}/`);

  return (
    <Link
      href={href}
      title={collapsed ? label : undefined}
      className={cn(
        "flex items-center gap-3 rounded-xl px-3 py-3 text-[15px] font-medium transition-colors",
        collapsed && "justify-center px-0",
        isActive
          ? "bg-[#eaf2ff] text-[#2563eb]"
          : "text-slate-900 hover:bg-slate-100/90",
      )}
    >
      <Icon className={cn("size-[18px] shrink-0", isActive ? "text-[#2563eb]" : "text-black")} />
      <span className={cn("truncate", collapsed && "hidden")}>{label}</span>
    </Link>
  );
}

export function AuthenticatedShell({ children }: AuthenticatedShellProps) {
  const [collapsed, setCollapsed] = useState(false);

  return (
    <div className="min-h-screen bg-transparent">
      <div className="flex min-h-screen flex-col lg:flex-row">
        <aside
          className={cn(
            "w-full border-b border-r border-slate-200 bg-white text-slate-900 lg:sticky lg:top-0 lg:h-screen lg:border-b-0",
            collapsed ? "lg:w-[88px]" : "lg:w-[280px]"
          )}
        >
          <div className="flex h-full flex-col p-3 lg:p-4">
            <div
              className={cn(
                "flex items-start",
                collapsed ? "justify-center" : "justify-between gap-3"
              )}
            >
              <Link href="/dashboard" className={cn("block", collapsed && "w-full")}>
                <FdsLogo
                  compact={collapsed}
                  hideText
                  logoSrc="/itc_logo_dark.png"
                  logoAlt="IT Consortium logo"
                  className={cn(collapsed ? "justify-center" : "justify-start")}
                />
              </Link>
              <Button
                variant="ghost"
                size="icon"
                type="button"
                aria-label="Collapse sidebar"
                onClick={() => setCollapsed(true)}
                className={cn(
                  "mt-1 hidden rounded-xl text-slate-500 hover:bg-slate-100 hover:text-slate-900 lg:inline-flex",
                  collapsed && "hidden"
                )}
              >
                <ChevronLeft className="size-4" />
              </Button>
            </div>

            <div className={cn("mt-8", collapsed && "mt-6")}>
              <div className="space-y-1">
                {primaryLinks.map((link) => (
                  <NavLink key={link.href} collapsed={collapsed} {...link} />
                ))}
              </div>
            </div>

            <div className={cn("mt-auto", collapsed && "mt-8")}>
              <div className="space-y-1">
                {secondaryLinks.map((link) => (
                  <NavLink key={link.href} collapsed={collapsed} {...link} />
                ))}
              </div>
              <Button
                variant="ghost"
                size="icon"
                type="button"
                aria-label="Expand sidebar"
                onClick={() => setCollapsed(false)}
                className={cn(
                  "mt-4 hidden rounded-xl text-slate-500 hover:bg-slate-100 hover:text-slate-900 lg:inline-flex",
                  !collapsed && "hidden"
                )}
              >
                <ChevronRight className="size-4" />
              </Button>
            </div>
          </div>
        </aside>

        <main className="flex-1 bg-white/90 p-4 shadow-[0_18px_48px_rgba(15,23,42,0.05)] backdrop-blur lg:p-6">
          {children}
        </main>
      </div>
    </div>
  );
}
