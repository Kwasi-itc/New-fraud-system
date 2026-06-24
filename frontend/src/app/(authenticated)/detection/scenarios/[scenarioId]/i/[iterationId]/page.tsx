import { redirect } from "next/navigation";

export default async function LegacyScenarioEditPage({
  params,
}: {
  params: Promise<{ scenarioId: string; iterationId: string }>;
}) {
  const { scenarioId, iterationId } = await params;

  redirect(`/detection/${scenarioId}/edit?iterationId=${iterationId}`);
}
