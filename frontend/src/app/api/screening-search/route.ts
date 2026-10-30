import { manualScreeningProxy } from "@/lib/server/manual-screening-proxy";
export async function POST(request: Request) { return manualScreeningProxy(request); }
export async function GET(request: Request) { return manualScreeningProxy(request); }
