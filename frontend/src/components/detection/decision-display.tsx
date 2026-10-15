"use client";

import { useQuery } from "@tanstack/react-query";
import { dataModelApi, type RecordDisplays } from "@/lib/data-model-api";
import type { Decision } from "@/lib/decision-engine-api";

export function useRecordDisplays(
  tenant: string,
  session: string,
  objects: Array<{ object_type: string; object_id: string }>,
  objectType?: string,
  enabled: boolean = true
) {
  const query = useQuery<RecordDisplays>({
    queryKey: ["record-displays", tenant, session, objects],
    queryFn: () => dataModelApi.readRecordDisplays(tenant, objects),
    enabled: enabled && objects.length > 0,
    retry: false,
    staleTime: 0,
    gcTime: 0,
  });

  const records = new Map(
    query.data?.records.map((record) => [JSON.stringify([record.object_type, record.object_id]), record])
  );
  const allColumns = query.data?.columns ?? {};
  const columns = objectType ? query.data?.columns[objectType] ?? [] : [];
  const record = (obj: { object_type: string; object_id: string }) =>
    records.get(JSON.stringify([obj.object_type, obj.object_id]));

  return { query, columns, allColumns, record, records };
}

export function useDecisionDisplay(
  tenant: string,
  session: string,
  decisions: Decision[],
  objectType?: string,
  enabled: boolean = true
) {
  const objects = decisions.map((d) => ({ object_type: d.object_type, object_id: d.object_id }));
  return useRecordDisplays(tenant, session, objects, objectType, enabled);
}

export function displayValue(value: unknown): string {
  if (value === null || value === undefined || value === "") return "—";
  if (typeof value === "boolean") return value ? "Yes" : "No";
  return typeof value === "object" ? JSON.stringify(value) : String(value);
}
