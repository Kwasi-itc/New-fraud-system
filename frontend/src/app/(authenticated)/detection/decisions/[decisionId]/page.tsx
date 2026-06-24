import { DecisionDetailPage } from "@/components/detection/decision-detail-page";

export default async function DetectionDecisionDetailRoute({
  params,
}: {
  params: Promise<{ decisionId: string }>;
}) {
  const { decisionId } = await params;

  return <DecisionDetailPage decisionId={decisionId} />;
}
