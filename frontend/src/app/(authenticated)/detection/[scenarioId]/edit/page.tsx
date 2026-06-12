import { ScenarioEditPage } from "@/components/detection/scenario-edit-page";

export default async function DetectionScenarioEditPage({
  params,
}: {
  params: Promise<{ scenarioId: string }>;
}) {
  const { scenarioId } = await params;

  return <ScenarioEditPage scenarioId={scenarioId} />;
}
