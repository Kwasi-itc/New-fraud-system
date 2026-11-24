import { redirect } from "next/navigation";
import { platformSession } from "@/lib/server/auth";
import { IdentityClientManager } from "@/components/identity-client-manager";
export default async function APIClientsPage() {
  const current = await platformSession(); if (!current) redirect("/login");
  const caps = current.session.capabilities;
  if (!caps.includes("clients:read")) return <p role="alert">Client administration access is required.</p>;
  return <IdentityClientManager capabilities={caps} canManage={caps.includes("clients:manage")} canApprove={caps.includes("clients:approve-sensitive")} actor={current.session.user_id} tenant={current.session.tenant_id} canManageInboxes={caps.includes("inboxes:manage")} />;
}
