import { platformProxy } from "@/lib/server/platform-proxy";
type Context = { params: Promise<{ path: string[] }> };
async function handle(request: Request, context: Context) {
  return platformProxy(request, (await context.params).path, "ingestion");
}
export { handle as GET, handle as POST, handle as PUT, handle as PATCH, handle as DELETE };
