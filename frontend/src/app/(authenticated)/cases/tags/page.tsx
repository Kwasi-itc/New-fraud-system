import { TagSettings } from "@/components/cases/tag-settings";
import { casePageContext } from "@/lib/server/case-page";

export default async function Page({ searchParams }: { searchParams: Promise<Record<string, string | string[] | undefined>> }) {
  const { tenant, session } = await casePageContext(searchParams);
  return <TagSettings key={`${tenant}:${session}`} tenant={tenant} session={session} />;
}
