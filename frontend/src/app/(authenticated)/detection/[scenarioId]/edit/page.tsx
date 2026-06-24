import { ScenarioEditPage } from "@/components/detection/scenario-edit-page";

export default async function DetectionScenarioEditPage({
  params,
  searchParams,
}: {
  params: Promise<{ scenarioId: string }>;
  searchParams?: Promise<{ iterationId?: string }>;
}) {
  const { scenarioId } = await params;
  const resolvedSearchParams = searchParams ? await searchParams : undefined;

  return (
    <ScenarioEditPage
      scenarioId={scenarioId}
      initialIterationId={resolvedSearchParams?.iterationId ?? null}
    />
  );
}
