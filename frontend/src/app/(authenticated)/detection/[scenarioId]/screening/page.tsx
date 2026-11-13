import { ScenarioScreeningPage } from "@/components/detection/scenario-screening-page";
export default async function Page({ params, searchParams }: {
  params: Promise<{ scenarioId: string }>;
  searchParams: Promise<{ create?: string | string[]; configId?: string | string[] }>;
}) {
  const { scenarioId } = await params;
  const query = await searchParams;
  const startCreating = query.create === "1";
  const initialConfigId = !startCreating && typeof query.configId === "string" ? query.configId : undefined;
  return <ScenarioScreeningPage key={`${scenarioId}:${startCreating}:${initialConfigId ?? ""}`} scenarioId={scenarioId} startCreating={startCreating} initialConfigId={initialConfigId} />;
}
