import { ManualScreening } from "@/components/screening/manual-screening";
import { platformSession } from "@/lib/server/auth";

export default async function ScreeningSearchPage() {
  const session = await platformSession();
  if (!session?.session.roles.some(role => ["case_investigator", "case_manager_admin"].includes(role))) {
    return <div className="p-8" role="alert">An investigator role is required to screen entities.</div>;
  }
  return <ManualScreening scope={`${session.session.tenant_id}:${session.session.user_id}`} />;
}
