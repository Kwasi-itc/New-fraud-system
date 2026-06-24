import { ScenarioTestPage } from "@/components/detection/scenario-test-page";

export default async function DetectionScenarioTestsPage({
  params,
}: {
  params: Promise<{ scenarioId: string }>;
}) {
  const { scenarioId } = await params;

  return <ScenarioTestPage scenarioId={scenarioId} />;
}
