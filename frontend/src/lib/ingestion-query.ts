import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { ingestionApi } from "@/lib/ingestion-api";

export const ingestionQueryKeys = {
  uploadLogs: (tenantId: string, objectType: string) =>
    ["ingestion", "upload-logs", tenantId, objectType] as const,
};

export function useUploadLogsQuery(tenantId: string, objectType: string) {
  return useQuery({
    queryKey: ingestionQueryKeys.uploadLogs(tenantId, objectType),
    queryFn: () => ingestionApi.listUploadLogs({ tenantId, objectType }),
    enabled: Boolean(tenantId) && Boolean(objectType),
    refetchInterval: (query) => {
      const logs = query.state.data?.upload_logs ?? [];
      const hasActiveLog = logs.some(
        (log) => log.status === "uploaded" || log.status === "processing"
      );
      return hasActiveLog ? 4000 : false;
    },
  });
}

export function useUploadCsvMutation(tenantId: string, objectType: string) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: ({ file, mode }: { file: File; mode?: "create" | "patch" }) =>
      ingestionApi.uploadCsv({ tenantId, objectType, file, mode }),
    onSuccess: () => {
      void queryClient.invalidateQueries({
        queryKey: ingestionQueryKeys.uploadLogs(tenantId, objectType),
      });
    },
  });
}
