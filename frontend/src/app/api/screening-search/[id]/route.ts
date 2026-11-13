import { manualScreeningProxy } from "@/lib/server/manual-screening-proxy";
export async function GET(request: Request, context: { params: Promise<{ id: string }> }) {
  return manualScreeningProxy(request, (await context.params).id);
}
