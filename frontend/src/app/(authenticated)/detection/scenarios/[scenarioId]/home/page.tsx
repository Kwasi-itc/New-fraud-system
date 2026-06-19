import { redirect } from "next/navigation";

export default async function LegacyScenarioHomePage({
  params,
}: {
  params: Promise<{ scenarioId: string }>;
}) {
  const { scenarioId } = await params;

  redirect(`/detection/${scenarioId}`);
}
