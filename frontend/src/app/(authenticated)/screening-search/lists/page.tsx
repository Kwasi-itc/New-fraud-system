import { PrivateScreeningLists } from "@/components/screening/private-lists";
import { platformSession } from "@/lib/server/auth";
export default async function PrivateListsPage() {
  const session = await platformSession();
  if (!session) return <p className="p-8" role="alert">Sign in to manage private lists.</p>;
  return <PrivateScreeningLists key={`${session.session.tenant_id}:${session.session.user_id}`} />;
}
