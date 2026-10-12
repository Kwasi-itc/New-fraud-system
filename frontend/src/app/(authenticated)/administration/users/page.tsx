import { redirect } from "next/navigation";
import { platformSession } from "@/lib/server/auth";
import { IdentityMembers } from "@/components/identity-members";
import { IdentityInvitations } from "@/components/identity-invitations";
import { IdentityAudit } from "@/components/identity-audit";
export default async function UsersPage() {
  const current = await platformSession(); if (!current) redirect("/login");
  if (!current.session.roles.includes("tenant_admin")) return <p role="alert">Account administrator access is required.</p>;
  return <div className="space-y-8"><h1 className="text-2xl font-semibold">Users &amp; access</h1><p>Manage access in your current tenant. Login credentials and MFA are managed by your sign-in provider.</p><IdentityInvitations /><IdentityMembers /><IdentityAudit /></div>;
}
