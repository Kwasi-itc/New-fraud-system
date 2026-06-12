import { ScenarioExecutionPage } from "@/components/detection/scenario-execution-page";

export default async function DetectionScenarioExecutionPage({
  params,
}: {
  params: Promise<{ scenarioId: string }>;
}) {
  const { scenarioId } = await params;

  return <ScenarioExecutionPage scenarioId={scenarioId} />;
}
