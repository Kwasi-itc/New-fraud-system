import { redirect } from "next/navigation";

export default async function LegacyScenarioDetailPage({
  params,
}: {
  params: Promise<{ scenarioId: string }>;
}) {
  const { scenarioId } = await params;

  redirect(`/detection/${scenarioId}`);
}
