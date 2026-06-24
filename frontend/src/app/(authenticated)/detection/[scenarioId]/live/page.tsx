import { ScenarioEditPage } from "@/components/detection/scenario-edit-page";

export default async function DetectionScenarioLiveIterationPage({
  params,
}: {
  params: Promise<{ scenarioId: string }>;
}) {
  const { scenarioId } = await params;

  return <ScenarioEditPage scenarioId={scenarioId} preferLiveIteration />;
}
