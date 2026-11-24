import { redirect } from "next/navigation";
import { platformSession } from "@/lib/server/auth";
import { IdentityRoleEditor } from "@/components/account/role-editor";
export default async function NewRolePage() {
  const current = await platformSession(); if (!current) redirect("/login");
  if (!current.session.capabilities.includes("roles:manage")) return <p role="alert">Tenant Administrator access is required.</p>;
  return <IdentityRoleEditor capabilities={current.session.capabilities} scope={`${current.session.tenant_id}:${current.session.user_id}`} />;
}
