import { ScenarioScreeningPage } from "@/components/detection/scenario-screening-page";
export default async function Page({ params }: { params: Promise<{ scenarioId: string }> }) {
  const { scenarioId } = await params;
  return <ScenarioScreeningPage key={scenarioId} scenarioId={scenarioId} />;
}
