import { ListDetailPage } from "@/components/detection/list-detail-page";

export default async function DetectionListDetailPage({
  params,
}: {
  params: Promise<{ listId: string }>;
}) {
  const { listId } = await params;

  return <ListDetailPage listId={listId} />;
}
