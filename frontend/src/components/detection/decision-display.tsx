"use client";

import { useQuery } from "@tanstack/react-query";
import { dataModelApi } from "@/lib/data-model-api";
import type { Decision } from "@/lib/decision-engine-api";

export function useDecisionDisplay(tenant: string, session: string, decisions: Decision[], objectType?: string) {
  const objects = decisions.map(d => ({ object_type: d.object_type, object_id: d.object_id }));
  const query = useQuery({ queryKey: ["record-displays", tenant, session, objects], queryFn: () => dataModelApi.readRecordDisplays(tenant, objects), enabled: objects.length > 0, retry: false, staleTime: 0, gcTime: 0 });
  const records = new Map(query.data?.records.map(record => [JSON.stringify([record.object_type, record.object_id]), record]));
  const columns = objectType ? query.data?.columns[objectType] ?? [] : [];
  const record = (decision: Decision) => records.get(JSON.stringify([decision.object_type, decision.object_id]));
  return { query, columns, record };
}

export function displayValue(value: unknown): string {
  if (value === null || value === undefined || value === "") return "—";
  if (typeof value === "boolean") return value ? "Yes" : "No";
  return typeof value === "object" ? JSON.stringify(value) : String(value);
}
