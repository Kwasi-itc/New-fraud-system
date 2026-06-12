import { ScenarioDetailPage } from "@/components/detection/scenario-detail-page";

export default async function DetectionScenarioPage({
  params,
}: {
  params: Promise<{ scenarioId: string }>;
}) {
  const { scenarioId } = await params;

  return <ScenarioDetailPage scenarioId={scenarioId} />;
}
