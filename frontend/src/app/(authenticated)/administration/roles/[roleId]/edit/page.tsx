import { redirect } from "next/navigation";
import { platformSession } from "@/lib/server/auth";
import { IdentityRoleEditor } from "@/components/account/role-editor";
export default async function EditRolePage({ params }: { params: Promise<{ roleId: string }> }) {
  const current = await platformSession(); if (!current) redirect("/login");
  if (!current.session.capabilities.includes("roles:manage")) return <p role="alert">Tenant Administrator access is required.</p>;
  const { roleId } = await params;
  return <IdentityRoleEditor roleId={roleId} capabilities={current.session.capabilities} scope={`${current.session.tenant_id}:${current.session.user_id}`} />;
}
