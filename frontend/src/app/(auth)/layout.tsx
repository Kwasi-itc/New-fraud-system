import type { ReactNode } from "react";

import { FdsLogo } from "@/components/fds-logo";

export default function AuthLayout({ children }: { children: ReactNode }) {
  return (
    <div className="min-h-screen bg-[radial-gradient(circle_at_top,rgba(37,99,235,0.08),transparent_24%),linear-gradient(180deg,#f7faff_0%,#eef4ff_100%)]">
      <div className="mx-auto flex min-h-screen max-w-6xl flex-col items-center justify-center px-6 py-8 sm:py-10">
        <FdsLogo
          stacked
          className="mb-3"
          logoSrc="/itc_logo_dark.png"
          logoAlt="IT Consortium logo"
          hideText
        />
        <p className="mb-8 text-center text-base font-medium text-slate-600 sm:mb-10 sm:text-lg">
          AML Compliance Platform
        </p>
        <section className="w-full max-w-[540px]">{children}</section>
      </div>
    </div>
  );
}
