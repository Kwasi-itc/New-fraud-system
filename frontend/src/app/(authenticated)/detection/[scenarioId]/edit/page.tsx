import { ScenarioEditPage } from "@/components/detection/scenario-edit-page";

export default async function DetectionScenarioEditPage({
  params,
  searchParams,
}: {
  params: Promise<{ scenarioId: string }>;
  searchParams?: Promise<{ iterationId?: string; tab?: string }>;
}) {
  const { scenarioId } = await params;
  const resolvedSearchParams = searchParams ? await searchParams : undefined;
  const initialTab =
    resolvedSearchParams?.tab === "Rules" || resolvedSearchParams?.tab === "Decision"
      ? resolvedSearchParams.tab
      : "Trigger";

  return (
    <ScenarioEditPage
      scenarioId={scenarioId}
      initialIterationId={resolvedSearchParams?.iterationId ?? null}
      initialTab={initialTab}
    />
  );
}
