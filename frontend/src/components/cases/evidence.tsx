"use client";

import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { ShieldAlert, Sparkles, ChevronDown, ChevronUp } from "lucide-react";
import { Button } from "@/components/ui/button";
import { caseKeys } from "@/lib/case-manager-query";
import { Problem } from "./shared";
import { StructuredDataViewer } from "./structured-data-viewer";

export function Evidence({
  tenant,
  session,
  caseId,
  kind,
  resource,
}: {
  tenant: string;
  session: string;
  caseId: string;
  kind: "decisions" | "screenings";
  resource: string;
}) {
  const [open, setOpen] = useState(false);
  const query = useQuery({
    queryKey: [...caseKeys.scope(tenant, session), caseId, "evidence", kind, resource],
    enabled: open,
    retry: false,
    staleTime: 0,
    gcTime: 0,
    queryFn: async () => {
      const response = await fetch(`/api/case-evidence/${tenant}/${caseId}/${kind}/${resource}`, {
        cache: "no-store",
      });
      const data = await response.json();
      if (!response.ok)
        throw new Error(
          typeof data.error === "string" ? data.error : data.error?.message ?? "Evidence is unavailable."
        );
      return data as unknown;
    },
  });

  return (
    <div className="space-y-3">
      <Button
        size="sm"
        variant="outline"
        onClick={() => setOpen(!open)}
        aria-expanded={open}
        className="gap-1.5 font-medium border-slate-200 hover:bg-slate-50 transition-colors"
      >
        <ShieldAlert className="size-3.5 text-blue-600" />
        {open ? "Hide evidence" : "Inspect evidence"}
        {open ? <ChevronUp className="size-3.5 text-slate-400" /> : <ChevronDown className="size-3.5 text-slate-400" />}
      </Button>

      {open && (
        <div className="rounded-xl border border-slate-200 bg-white p-4 text-sm shadow-xs transition-all">
          <Problem error={query.error} retry={() => void query.refetch()} />
          {query.isPending && (
            <div className="flex items-center gap-2 py-4 text-slate-500">
              <span className="inline-block size-3 animate-spin rounded-full border-2 border-blue-600 border-t-transparent" />
              <p role="status">Loading source evidence…</p>
            </div>
          )}
          {query.isSuccess && (
            <StructuredDataViewer
              data={query.data}
              title={kind === "decisions" ? "Decision Evidence Snapshot" : "Screening Evidence Snapshot"}
            />
          )}
        </div>
      )}
    </div>
  );
}
