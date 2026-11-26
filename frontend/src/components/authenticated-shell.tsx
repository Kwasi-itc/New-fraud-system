"use client";

import { Suspense, useEffect, useState, type ComponentType, type ReactNode } from "react";
import Link from "next/link";
import { usePathname, useSearchParams } from "next/navigation";
import {
  ChevronLeft,
  ChevronRight,
  Database,
  LayoutDashboard,
  List,
  Route,
  Scale,
  Shield,
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
    href: "/dashboard",
    label: "Dashboard",
    icon: LayoutDashboard,
  },
  {
    href: "/your-data",
    label: "Your Data Model",
    icon: Database,
  },
];

const secondaryLinks = [
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
      aria-current={isActive ? "page" : undefined}
      title={collapsed ? label : undefined}
      className={cn(
        "flex items-center gap-3 rounded-[9px] px-3 py-2.5 text-sm font-medium transition-colors",
        collapsed && "justify-center px-0",
        isActive
          ? "bg-[#eef4ff] text-[#1f4f96]"
          : "text-[#526174] hover:bg-slate-100/90",
      )}
    >
      <Icon className={cn("size-[18px] shrink-0", isActive ? "text-[#1f4f96]" : "text-[#71849e]")} />
      <span className={cn("truncate", collapsed && "hidden")}>{label}</span>
    </Link>
  );
}

const detectionLinks = [
  {
    href: "/detection",
    label: "Scenarios",
    icon: Route,
    tab: "Scenarios",
  },
  {
    href: "/detection?tab=Lists",
    label: "Lists",
    icon: List,
    tab: "Lists",
  },
  {
    href: "/detection?tab=Decisions",
    label: "Decisions",
    icon: Scale,
    tab: "Decisions",
  },
] as const;

function DetectionNavigation({ collapsed }: { collapsed: boolean }) {
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const requestedTab = searchParams.get("tab");
  const activeTab = pathname.startsWith("/detection/decisions/")
    ? "Decisions"
    : pathname.startsWith("/detection/lists/")
      ? "Lists"
      : requestedTab === "Lists" || requestedTab === "Decisions" ? requestedTab : "Scenarios";
  const isDetectionPath = pathname.startsWith("/detection");

  return (
    <div className="space-y-1">
      <div
        className={cn(
          "flex items-center gap-3 px-3 pb-1 pt-3 text-[11px] font-semibold uppercase tracking-[0.13em] text-slate-400",
          collapsed && "justify-center px-0"
        )}
      >
        <Shield className="size-4 shrink-0" />
        <span className={cn(collapsed && "hidden")}>Detection</span>
      </div>
      {detectionLinks.map((link) => {
        const Icon = link.icon;
        const active = isDetectionPath && activeTab === link.tab;

        return (
          <Link
            key={link.href}
            href={link.href}
            aria-current={active ? "page" : undefined}
            title={collapsed ? link.label : undefined}
            className={cn(
              "flex items-center gap-3 rounded-[9px] px-3 py-2.5 text-sm font-medium transition-colors",
              collapsed ? "justify-center px-0" : "pl-6",
              active
                ? "bg-[#eef4ff] text-[#1f4f96]"
                : "text-[#526174] hover:bg-slate-100/90"
            )}
          >
            <Icon
              className={cn(
                "size-[17px] shrink-0",
                active ? "text-[#1f4f96]" : "text-[#71849e]"
              )}
            />
            <span className={cn("truncate", collapsed && "hidden")}>{link.label}</span>
          </Link>
        );
      })}
    </div>
  );
}

export function AuthenticatedShell({ children }: AuthenticatedShellProps) {
  const [collapsed, setCollapsed] = useState(false);
  const pathname = usePathname();
  useEffect(() => {
    const mobile = window.matchMedia("(max-width: 1023px)");
    const expandOnMobile = () => { if (mobile.matches) setCollapsed(false); };
    mobile.addEventListener("change", expandOnMobile);
    expandOnMobile();
    return () => mobile.removeEventListener("change", expandOnMobile);
  }, []);
  const excludedArea = ["/my-account", "/administration", "/screening-search", "/cases", "/investigator", "/settings"]
    .some((path) => pathname === path || pathname.startsWith(`${path}/`)) ||
    /^\/detection\/[^/]+\/screening(?:\/|$)/.test(pathname);

  return (
    <div className="min-h-screen bg-transparent">
      <div className="flex min-h-screen flex-col lg:flex-row">
        <aside
          className={cn(
            "w-full border-b border-r border-slate-200 bg-white text-slate-900 lg:sticky lg:top-0 lg:h-screen lg:border-b-0",
            collapsed ? "lg:w-[64px]" : "lg:w-[224px]"
          )}
        >
          <div className="flex h-full flex-col p-3 lg:p-4">
            <div
              className={cn(
                "flex items-start",
                collapsed ? "justify-center" : "justify-between gap-3"
              )}
            >
              <Link href="/dashboard" className="block min-w-0 flex-1">
                <FdsLogo
                  compact={collapsed}
                  hideText
                  logoSrc="/itc_logo_dark.png"
                  logoAlt="IT Consortium logo"
                  className={cn(collapsed
                    ? "justify-center [&>div]:w-10 [&>div]:overflow-hidden [&_img]:!w-[180px] [&_img]:!max-w-none [&_img]:!object-left"
                    : "justify-start [&>div]:w-full")}
                />
              </Link>
              <Button
                variant="ghost"
                size="icon"
                type="button"
                aria-label="Collapse sidebar"
                onClick={() => setCollapsed(true)}
                className={cn(
                  "mt-1 hidden shrink-0 rounded-xl text-slate-500 hover:bg-slate-100 hover:text-slate-900",
                  !collapsed && "lg:inline-flex"
                )}
              >
                <ChevronLeft className="size-4" />
              </Button>
            </div>

            <nav aria-label="Product navigation" className={cn("mt-8", collapsed && "mt-6")}>
              <div className="space-y-1">
                <div className={cn("px-3 pb-2 text-[10px] font-bold uppercase tracking-[0.09em] text-slate-400", collapsed && "sr-only")}>Workspace</div>
                {primaryLinks.map((link) => (
                  <NavLink key={link.href} collapsed={collapsed} {...link} />
                ))}
                <Suspense fallback={null}>
                  <DetectionNavigation collapsed={collapsed} />
                </Suspense>
              </div>
            </nav>

            <div className={cn("mt-auto border-t border-slate-200 pt-4", collapsed && "mt-8")}>
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
                  "mt-4 hidden rounded-xl text-slate-500 hover:bg-slate-100 hover:text-slate-900",
                  collapsed && "lg:inline-flex"
                )}
              >
                <ChevronRight className="size-4" />
              </Button>
            </div>
          </div>
        </aside>

        <main className={cn("min-w-0 flex-1 p-4 lg:p-6", excludedArea ? "bg-white/90 shadow-[0_18px_48px_rgba(15,23,42,0.05)] backdrop-blur" : "product-workspace bg-[#f7f9fc] lg:px-[38px] lg:pb-[70px] lg:pt-[27px]")}>
          {children}
        </main>
      </div>
    </div>
  );
}
