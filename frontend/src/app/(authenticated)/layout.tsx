import type { ReactNode } from "react";

import { AuthenticatedShell } from "@/components/authenticated-shell";
import { platformSession } from "@/lib/server/auth";
import { redirect } from "next/navigation";

export default async function AuthenticatedLayout({
  children,
}: {
  children: ReactNode;
}) {
  if (!(await platformSession())) redirect("/login");
  return <AuthenticatedShell>{children}</AuthenticatedShell>;
}
