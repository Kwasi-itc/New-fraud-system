import { manualScreeningProxy } from "@/lib/server/manual-screening-proxy";
export async function GET(request: Request) { return manualScreeningProxy(request, "capabilities"); }
