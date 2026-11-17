import { manualScreeningProxy } from "@/lib/server/manual-screening-proxy";

export async function POST(request: Request, context: { params: Promise<{ id: string; matchId: string }> }) {
  const { id, matchId } = await context.params;
  return manualScreeningProxy(request, id, matchId);
}
