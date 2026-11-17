import { ScreeningHistoryPage } from "@/components/screening/screening-history-page";
import { platformSession } from "@/lib/server/auth";

export default async function ScreeningHistoryRoute() {
  const session = await platformSession();
  if (!session?.session.roles.some(role => ["case_investigator", "case_manager_admin"].includes(role))) {
    return <div className="p-8" role="alert">An investigator role is required to screen entities.</div>;
  }
  return <ScreeningHistoryPage scope={`${session.session.tenant_id}:${session.session.user_id}`} />;
}

