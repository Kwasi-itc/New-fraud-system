"use client";
import Link from "next/link";
import type { ReactNode } from "react";
import { Button } from "@/components/ui/button";

export const fieldClass = "w-full rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm";
export const labelClass = "grid gap-1 text-sm font-medium text-slate-700";
export function ClosingFindings({ label }: { label: string }) {
  return <label className={labelClass}>{label}<textarea aria-label={label} name="comment" required rows={3} className={fieldClass}
    onInput={event => {
      const field = event.currentTarget;
      field.setCustomValidity(!field.value.trim() ? "Enter closing findings, not just spaces." : new TextEncoder().encode(field.value).length > 10000 ? "Closing findings must be at most 10,000 bytes. Shorten the text." : "");
    }} /><span className="text-xs font-normal text-slate-500">Required. Maximum 10,000 bytes; some characters use more than one byte.</span></label>;
}
export function Panel({ title, children }: { title: string; children: ReactNode }) {
  return <section className="space-y-4 rounded-xl border border-slate-200 bg-white p-5"><h2 className="text-lg font-semibold">{title}</h2>{children}</section>;
}
export function Problem({ error, retry }: { error: Error | null; retry?: () => void }) {
  return error ? <div role="alert" className="rounded-lg border border-red-200 bg-red-50 p-3 text-sm text-red-800">{error.message} {retry && <Button size="sm" variant="outline" onClick={retry}>Retry</Button>}</div> : null;
}
export function CaseNav({ tenant, queue }: { tenant: string; queue?: string }) { return <nav className="flex gap-5 text-sm font-medium text-blue-700"><Link href={`/cases?${new URLSearchParams({ tenant, ...(queue !== undefined ? { queue } : {}) })}`}>Case queue</Link><Link href={`/cases/analytics?tenant=${tenant}`}>Analytics</Link><Link href={`/cases/inboxes?tenant=${tenant}`}>Inbox settings</Link><Link href={`/cases/tags?tenant=${tenant}`}>Tag settings</Link></nav>; }
export function readable(value?: string) { return value?.replaceAll("_", " ") ?? "—"; }
export function timestamp(value: string) { return new Date(value).toLocaleString(); }
