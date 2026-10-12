import { DecisionCase } from "@/components/cases/decision-case";
import { casePageContext } from "@/lib/server/case-page";

export default async function DecisionCasePage({ params, searchParams }: { params: Promise<{ decisionId: string }>; searchParams: Promise<Record<string, string | string[] | undefined>> }) {
  const { tenant, session } = await casePageContext(searchParams);
  const { decisionId } = await params;
  return <DecisionCase key={`${tenant}:${session}:${decisionId}`} tenant={tenant} session={session} decisionId={decisionId} />;
}
