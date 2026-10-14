import { redirect } from "next/navigation";
import DetectionWorkspace from "@/components/detection/detection-workspace";
import { platformSession } from "@/lib/server/auth";

export default async function DetectionPage() {
  const current = await platformSession();
  if (!current) redirect("/login");
  const tenant = process.env.NEXT_PUBLIC_DATA_MODEL_TENANT_ID || current.session.tenant_id;
  return <DetectionWorkspace tenant={tenant} session={`${current.session.id}:${current.session.version}`} />;
}
