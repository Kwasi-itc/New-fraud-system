import { CaseDetail } from "@/components/cases/case-detail";
import { casePageContext } from "@/lib/server/case-page";
export default async function CasePage({ params, searchParams }: { params: Promise<{ caseId: string }>; searchParams: Promise<Record<string, string | string[] | undefined>> }) {
  const { tenant, session, params: query } = await casePageContext(searchParams);
  const { caseId } = await params;
  return <CaseDetail key={`${tenant}:${session}:${caseId}`} tenant={tenant} session={session} id={caseId} queueFilters={typeof query.queue === "string" ? query.queue : ""} />;
}
