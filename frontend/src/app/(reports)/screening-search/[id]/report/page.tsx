import { redirect, notFound } from "next/navigation";
import { platformSession } from "@/lib/server/auth";
import { ScreeningReport } from "@/components/screening/screening-report";

export default async function ScreeningReportPage({ params }: { params: Promise<{ id: string }> }) {
  const session = await platformSession();
  if (!session) redirect("/login");
  if (!session.session.roles.some(role => ["case_investigator", "case_manager_admin"].includes(role))) {
    return <p role="alert" className="p-8">An investigator role is required to view screening reports.</p>;
  }
  const { id } = await params;
  if (!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(id)) notFound();
  return <ScreeningReport id={id} scope={`${session.session.tenant_id}:${session.session.user_id}`} />;
}
