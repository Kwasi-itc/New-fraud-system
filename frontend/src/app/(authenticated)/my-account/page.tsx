import Link from "next/link";
import { redirect } from "next/navigation";
import { platformSession } from "@/lib/server/auth";
import { IdentityMembers } from "@/components/identity-members";
import { IdentitySessions } from "@/components/identity-sessions";
import { identityRoleLabels } from "@/lib/identity-roles";
export default async function MyAccountPage() {
  const current = await platformSession(); if (!current) redirect("/login");
  const { session } = current;
  const labels = identityRoleLabels;
  return <div className="space-y-6"><h1 className="text-2xl font-semibold">My account</h1><p>{session.name}</p><p>{session.roles.map(role => labels[role] ?? role).join(", ")}</p>
    {session.roles.some(role => ["case_investigator", "case_manager_admin"].includes(role)) && <Link className="underline" href={`/cases?tenant=${session.tenant_id}`}>Open Case Manager</Link>}
    <form action="/api/auth/logout" method="post"><button className="rounded border px-4 py-2" type="submit">Sign out</button></form>
    <IdentitySessions />
    {session.roles.includes("tenant_admin") && <Link className="underline" href="/administration/users">Manage users, invitations and access</Link>}
    {session.roles.includes("tenant_admin") && <IdentityMembers />}
  </div>;
}
