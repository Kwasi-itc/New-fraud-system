import type { ReactNode } from "react";

import { AuthenticatedShell } from "@/components/authenticated-shell";
import { platformSession } from "@/lib/server/auth";
import { redirect } from "next/navigation";
import { WorkspaceSession } from "@/components/workspace-session";

export default async function AuthenticatedLayout({
  children,
}: {
  children: ReactNode;
}) {
  const identity = await platformSession();
  if (!identity) redirect("/login");
  const session = identity.session;
  return <WorkspaceSession key={`${session.id}:${session.tenant_id}:${session.version}`} tenant={session.tenant_id}><AuthenticatedShell>{children}</AuthenticatedShell></WorkspaceSession>;
}
