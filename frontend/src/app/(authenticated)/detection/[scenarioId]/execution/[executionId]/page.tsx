import { ScheduledExecutionDetailPage } from "@/components/detection/scheduled-execution-detail-page";

export default async function DetectionScheduledExecutionDetailPage({
  params,
}: {
  params: Promise<{ scenarioId: string; executionId: string }>;
}) {
  const { scenarioId, executionId } = await params;

  return (
    <ScheduledExecutionDetailPage
      scenarioId={scenarioId}
      executionId={executionId}
    />
  );
}
