"use client";
import Link from "next/link";
import { useRouter } from "next/navigation";
import type { ReactNode } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";

export const fieldClass = "w-full rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm";
export const labelClass = "grid gap-1 text-sm font-medium text-slate-700";
export function Panel({ title, children }: { title: string; children: ReactNode }) {
  return <section className="space-y-4 rounded-xl border border-slate-200 bg-white p-5"><h2 className="text-lg font-semibold">{title}</h2>{children}</section>;
}
export function Problem({ error, retry }: { error: Error | null; retry?: () => void }) {
  return error ? <div role="alert" className="rounded-lg border border-red-200 bg-red-50 p-3 text-sm text-red-800">{error.message} {retry && <Button size="sm" variant="outline" onClick={retry}>Retry</Button>}</div> : null;
}
export function TenantField({ tenant }: { tenant: string }) {
  const router = useRouter();
  return <form className="flex items-end gap-2" onSubmit={event => { event.preventDefault(); const next = String(new FormData(event.currentTarget).get("tenant") ?? "").trim(); router.push(`/cases?tenant=${encodeURIComponent(next)}`); }}>
    <label className={labelClass}>Tenant<Input name="tenant" defaultValue={tenant} required pattern="[0-9a-fA-F-]{36}" className="w-80" /></label><Button variant="outline" type="submit">Switch tenant</Button>
  </form>;
}
export function CaseNav({ tenant }: { tenant: string }) { return <nav className="flex gap-5 text-sm font-medium text-blue-700"><Link href={`/cases?tenant=${tenant}`}>Case queue</Link><Link href={`/cases/analytics?tenant=${tenant}`}>Analytics</Link><Link href={`/cases/inboxes?tenant=${tenant}`}>Inbox settings</Link><Link href={`/cases/tags?tenant=${tenant}`}>Tag settings</Link></nav>; }
export function readable(value?: string) { return value?.replaceAll("_", " ") ?? "—"; }
export function timestamp(value: string) { return new Date(value).toLocaleString(); }
