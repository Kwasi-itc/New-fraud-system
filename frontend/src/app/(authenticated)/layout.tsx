import type { ReactNode } from "react";

import { AuthenticatedShell } from "@/components/authenticated-shell";
import { platformSession } from "@/lib/server/auth";
import { redirect } from "next/navigation";
import { localMySQLSession } from "@/lib/server/local-mysql-session";
import { WorkspaceSession } from "@/components/workspace-session";

export default async function AuthenticatedLayout({
  children,
}: {
  children: ReactNode;
}) {
  const current = await platformSession();
  if (!current) redirect("/login");
  const scope = `${current.session.id}:${current.session.version}:${current.session.tenant_id}`;
  return <WorkspaceSession key={scope} scope={scope} tenant={current.session.tenant_id}><AuthenticatedShell>
    {localMySQLSession() && <div role="status" className="mb-4 rounded border border-amber-300 bg-amber-50 p-3 text-sm text-amber-950">Local MySQL development — authentication is disabled.</div>}
    {children}
  </AuthenticatedShell></WorkspaceSession>;
}
