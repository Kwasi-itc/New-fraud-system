import { ScenarioWorkflowPage } from "@/components/detection/scenario-workflow-page";

export default async function DetectionScenarioWorkflowPage({
  params,
}: {
  params: Promise<{ scenarioId: string }>;
}) {
  const { scenarioId } = await params;

  return <ScenarioWorkflowPage scenarioId={scenarioId} />;
}
