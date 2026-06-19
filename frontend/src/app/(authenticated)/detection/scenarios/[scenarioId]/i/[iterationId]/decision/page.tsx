import { redirect } from "next/navigation";

export default async function LegacyScenarioDecisionPage({
  params,
}: {
  params: Promise<{ scenarioId: string; iterationId: string }>;
}) {
  const { scenarioId } = await params;

  redirect(`/detection/${scenarioId}/edit`);
}
