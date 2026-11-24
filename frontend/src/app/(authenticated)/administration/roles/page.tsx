import { redirect } from "next/navigation";
import { platformSession } from "@/lib/server/auth";
import { IdentityRoleManager } from "@/components/identity-role-manager";
export default async function RolesPage({ searchParams }: { searchParams: Promise<{ role?: string }> }) {
  const current = await platformSession(); if (!current) redirect("/login");
  if (!current.session.capabilities.includes("roles:manage")) return <p role="alert">Tenant Administrator access is required.</p>;
  const { role } = await searchParams;
  return <IdentityRoleManager capabilities={current.session.capabilities} scope={`${current.session.tenant_id}:${current.session.user_id}`} initialRoleId={role} />;
}
