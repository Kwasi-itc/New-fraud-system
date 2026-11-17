import { manualScreeningProxy } from "@/lib/server/manual-screening-proxy";

async function handle(request: Request, { params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return manualScreeningProxy(request, id, undefined, "monitor");
}
export { handle as GET, handle as POST };
