import { RuleDetailPage } from "@/components/detection/rule-detail-page";

export default async function DetectionRuleDetailPage({
  params,
}: {
  params: Promise<{ scenarioId: string; ruleId: string }>;
}) {
  const { scenarioId, ruleId } = await params;

  return <RuleDetailPage scenarioId={scenarioId} ruleId={ruleId} />;
}
