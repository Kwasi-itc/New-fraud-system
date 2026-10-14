import { DecisionDetailPage } from "@/components/detection/decision-detail-page";
import { casePageContext } from "@/lib/server/case-page";

export default async function DetectionDecisionDetailRoute({
  params,
  searchParams,
}: {
  params: Promise<{ decisionId: string }>;
  searchParams: Promise<Record<string, string | string[] | undefined>>;
}) {
  const { decisionId } = await params;
  const { tenant, session } = await casePageContext(searchParams);

  return <DecisionDetailPage key={`${tenant}:${session}:${decisionId}`} decisionId={decisionId} tenantId={tenant} session={session} />;
}
