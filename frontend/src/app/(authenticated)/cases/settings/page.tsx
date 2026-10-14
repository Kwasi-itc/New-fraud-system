import { CaseSettings } from "@/components/cases/case-settings";
import { casePageContext } from "@/lib/server/case-page";

export default async function Page({
  searchParams,
}: {
  searchParams: Promise<Record<string, string | string[] | undefined>>;
}) {
  const { tenant, session } = await casePageContext(searchParams);
  const resolvedParams = await searchParams;
  const tab = resolvedParams.tab === "tags" ? "tags" : "inboxes";
  return (
    <CaseSettings
      key={`${tenant}:${session}:${tab}`}
      tenant={tenant}
      session={session}
      defaultTab={tab}
    />
  );
}
