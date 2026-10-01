"use client";
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { requestCase, writeCase, type CaseSession, type Inbox, type Tag, type QueuePage, type LinkPage, type ReportPage } from "./case-manager-api";

export const caseKeys = { scope: (tenant: string, session: string) => ["cases", tenant, session] as const };
export function useCaseReports(tenant: string, session: string, id: string) {
  return useInfiniteQuery({ queryKey: [...caseKeys.scope(tenant, session), id, "reports"], initialPageParam: "",
    queryFn: ({ pageParam }) => requestCase<ReportPage>(tenant, `cases/${id}/reports?limit=20${pageParam ? `&cursor=${encodeURIComponent(pageParam)}` : ""}`),
    getNextPageParam: page => page.next_cursor, enabled: !!tenant, retry: false, staleTime: 0, gcTime: 0, refetchOnWindowFocus: true });
}
export function useCaseQuery<T>(tenant: string, session: string, path: string, enabled = true) {
  return useQuery({ queryKey: [...caseKeys.scope(tenant, session), path], queryFn: () => requestCase<T>(tenant, path), enabled: !!tenant && enabled, retry: false, staleTime: 0, gcTime: 0, refetchOnWindowFocus: true });
}
export function useCaseContext(tenant: string, session: string) {
  const identity = useCaseQuery<CaseSession>(tenant, session, "case-session");
  const inboxes = useCaseQuery<{ inboxes: Inbox[] }>(tenant, session, "inboxes", identity.isSuccess);
  const tags = useCaseQuery<{ tags: Tag[] }>(tenant, session, "tags?target=case", identity.isSuccess);
  return { identity, inboxes, tags };
}
export function useCaseQueue(tenant: string, session: string, filters: string, enabled = true) {
  return useInfiniteQuery({ queryKey: [...caseKeys.scope(tenant, session), "queue", filters],
    initialPageParam: "", queryFn: ({ pageParam }) => requestCase<QueuePage>(tenant, `case-queue?${filters}&limit=50${pageParam ? `&cursor=${encodeURIComponent(pageParam)}` : ""}`),
    getNextPageParam: page => page.next_cursor, enabled: !!tenant && enabled, retry: false, staleTime: 0, gcTime: 0, refetchOnWindowFocus: true });
}
export function useCaseLinks<T>(tenant: string, session: string, id: string, kind: string) {
  return useInfiniteQuery({ queryKey: [...caseKeys.scope(tenant, session), id, kind], initialPageParam: "",
    queryFn: ({ pageParam }) => requestCase<LinkPage<T>>(tenant, `cases/${id}/links/${kind}?limit=50${pageParam ? `&cursor=${encodeURIComponent(pageParam)}` : ""}`),
    getNextPageParam: page => page.next_cursor, enabled: !!tenant, retry: false, staleTime: 0, gcTime: 0, refetchOnWindowFocus: true });
}
export function useCaseWrite(tenant: string, session: string) {
  const client = useQueryClient();
  return useMutation({ mutationFn: ({ path, payload, method }: { path: string; payload?: unknown; method?: string }) => writeCase(tenant, path, payload, method),
    onSuccess: async () => { await client.invalidateQueries({ queryKey: caseKeys.scope(tenant, session) }); } });
}
