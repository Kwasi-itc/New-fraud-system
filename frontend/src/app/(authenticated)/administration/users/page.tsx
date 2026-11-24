import { redirect } from "next/navigation";
import { platformSession } from "@/lib/server/auth";
import { AccountUsers } from "@/components/account/account-users";
export default async function UsersPage() {
  const current = await platformSession(); if (!current) redirect("/login");
  if (!current.session.capabilities.includes("roles:assign")) return <p role="alert">Tenant Administrator access is required.</p>;
  return <AccountUsers capabilities={current.session.capabilities} tenant={current.session.tenant_id} actor={current.session.user_id} />;
}
