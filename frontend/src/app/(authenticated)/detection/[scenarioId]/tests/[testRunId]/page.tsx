import { ScenarioTestRunDetailPage } from "@/components/detection/scenario-test-run-detail-page";

export default async function DetectionScenarioTestRunDetailPage({
  params,
}: {
  params: Promise<{ scenarioId: string; testRunId: string }>;
}) {
  const { scenarioId, testRunId } = await params;

  return <ScenarioTestRunDetailPage scenarioId={scenarioId} testRunId={testRunId} />;
}
