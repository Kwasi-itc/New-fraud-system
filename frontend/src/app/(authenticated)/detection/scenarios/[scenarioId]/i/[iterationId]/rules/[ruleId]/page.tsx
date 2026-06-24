import { redirect } from "next/navigation";

export default async function LegacyScenarioRuleDetailPage({
  params,
}: {
  params: Promise<{ scenarioId: string; iterationId: string; ruleId: string }>;
}) {
  const { scenarioId, iterationId, ruleId } = await params;

  redirect(`/detection/${scenarioId}/edit/rules/${ruleId}?iterationId=${iterationId}`);
}
