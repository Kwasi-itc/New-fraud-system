import { privateListProxy } from "@/lib/server/private-list-proxy";
async function handle(request: Request, { params }: { params: Promise<{ path?: string[] }> }) {
  return privateListProxy(request, (await params).path);
}
export { handle as GET, handle as POST };
