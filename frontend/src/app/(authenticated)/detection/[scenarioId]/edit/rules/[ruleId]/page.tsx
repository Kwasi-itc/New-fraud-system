import { RuleDetailPage } from "@/components/detection/rule-detail-page";

export default async function DetectionRuleDetailPage({
  params,
  searchParams,
}: {
  params: Promise<{ scenarioId: string; ruleId: string }>;
  searchParams?: Promise<{ iterationId?: string }>;
}) {
  const { scenarioId, ruleId } = await params;
  const resolvedSearchParams = searchParams ? await searchParams : undefined;

  return (
    <RuleDetailPage
      scenarioId={scenarioId}
      ruleId={ruleId}
      initialIterationId={resolvedSearchParams?.iterationId ?? null}
    />
  );
}
