import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import {
  type CreateFieldRequest,
  type CreateTableRequest,
  type CreateLinkRequest,
  type CreatePivotRequest,
  dataModelApi,
  type CreateFieldEnumValueRequest,
  type PortableDataModelDocument,
  type UpdateFieldEnumValueRequest,
  type UpdateFieldRequest,
  type UpdateTableRequest,
} from "@/lib/data-model-api";

export const dataModelQueryKeys = {
  tenant: (tenantId: string) => ["data-model", "tenant", tenantId] as const,
  assembledModel: (tenantId: string) =>
    ["data-model", "assembled-model", tenantId] as const,
  tables: (tenantId: string) => ["data-model", "tables", tenantId] as const,
  pivots: (tenantId: string) => ["data-model", "pivots", tenantId] as const,
  schemaChanges: (tenantId: string) =>
    ["data-model", "schema-changes", tenantId] as const,
  indexJobs: (tenantId: string) => ["data-model", "index-jobs", tenantId] as const,
};

export function useTenantQuery(tenantId: string) {
  return useQuery({
    queryKey: dataModelQueryKeys.tenant(tenantId),
    queryFn: () => dataModelApi.getTenant(tenantId),
    enabled: Boolean(tenantId),
  });
}

export function useAssembledDataModelQuery(tenantId: string) {
  return useQuery({
    queryKey: dataModelQueryKeys.assembledModel(tenantId),
    queryFn: () => dataModelApi.getAssembledDataModel(tenantId),
    enabled: Boolean(tenantId),
  });
}

export function useTablesQuery(tenantId: string) {
  return useQuery({
    queryKey: dataModelQueryKeys.tables(tenantId),
    queryFn: () => dataModelApi.listTables(tenantId),
    enabled: Boolean(tenantId),
  });
}

export function useSchemaChangesQuery(tenantId: string) {
  return useQuery({
    queryKey: dataModelQueryKeys.schemaChanges(tenantId),
    queryFn: () => dataModelApi.listSchemaChanges(tenantId),
    enabled: Boolean(tenantId),
  });
}

export function usePivotsQuery(tenantId: string) {
  return useQuery({
    queryKey: dataModelQueryKeys.pivots(tenantId),
    queryFn: () => dataModelApi.listPivots(tenantId),
    enabled: Boolean(tenantId),
  });
}

export function useIndexJobsQuery(tenantId: string) {
  return useQuery({
    queryKey: dataModelQueryKeys.indexJobs(tenantId),
    queryFn: () => dataModelApi.listIndexJobs(tenantId),
    enabled: Boolean(tenantId),
  });
}

export function useCreateTableMutation(tenantId: string) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (payload: CreateTableRequest) =>
      dataModelApi.createTable(tenantId, payload),
    onSuccess: () => {
      invalidateTableQueries(queryClient, tenantId);
    },
  });
}

function invalidateTableQueries(queryClient: ReturnType<typeof useQueryClient>, tenantId: string) {
  void queryClient.invalidateQueries({
    queryKey: dataModelQueryKeys.tables(tenantId),
  });
  void queryClient.invalidateQueries({
    queryKey: dataModelQueryKeys.pivots(tenantId),
  });
  void queryClient.invalidateQueries({
    queryKey: dataModelQueryKeys.assembledModel(tenantId),
  });
  void queryClient.invalidateQueries({
    queryKey: dataModelQueryKeys.schemaChanges(tenantId),
  });
  void queryClient.invalidateQueries({
    queryKey: dataModelQueryKeys.indexJobs(tenantId),
  });
}

export function useUpdateTableMutation(tenantId: string) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: ({ tableId, payload }: { tableId: string; payload: UpdateTableRequest }) =>
      dataModelApi.updateTable(tableId, payload),
    onSuccess: () => {
      invalidateTableQueries(queryClient, tenantId);
    },
  });
}

export function useDeleteTableMutation(tenantId: string) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (tableId: string) => dataModelApi.deleteTable(tableId),
    onSuccess: () => {
      invalidateTableQueries(queryClient, tenantId);
    },
  });
}

export function useCreateFieldMutation(tenantId: string) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: ({ tableId, payload }: { tableId: string; payload: CreateFieldRequest }) =>
      dataModelApi.createField(tableId, payload),
    onSuccess: () => {
      invalidateTableQueries(queryClient, tenantId);
    },
  });
}

export function useUpdateFieldMutation(tenantId: string) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: ({ fieldId, payload }: { fieldId: string; payload: UpdateFieldRequest }) =>
      dataModelApi.updateField(fieldId, payload),
    onSuccess: () => {
      invalidateTableQueries(queryClient, tenantId);
    },
  });
}

export function useDeleteFieldMutation(tenantId: string) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (fieldId: string) => dataModelApi.deleteField(fieldId),
    onSuccess: () => {
      invalidateTableQueries(queryClient, tenantId);
    },
  });
}

export function useCreateFieldEnumValueMutation(tenantId: string) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: ({
      fieldId,
      payload,
    }: {
      fieldId: string;
      payload: CreateFieldEnumValueRequest;
    }) => dataModelApi.createFieldEnumValue(fieldId, payload),
    onSuccess: () => {
      invalidateTableQueries(queryClient, tenantId);
    },
  });
}

export function useUpdateFieldEnumValueMutation(tenantId: string) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: ({
      enumValueId,
      payload,
    }: {
      enumValueId: string;
      payload: UpdateFieldEnumValueRequest;
    }) => dataModelApi.updateFieldEnumValue(enumValueId, payload),
    onSuccess: () => {
      invalidateTableQueries(queryClient, tenantId);
    },
  });
}

export function useDeleteFieldEnumValueMutation(tenantId: string) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (enumValueId: string) => dataModelApi.deleteFieldEnumValue(enumValueId),
    onSuccess: () => {
      invalidateTableQueries(queryClient, tenantId);
    },
  });
}

export function useCreateLinkMutation(tenantId: string) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (payload: CreateLinkRequest) => dataModelApi.createLink(tenantId, payload),
    onSuccess: () => {
      invalidateTableQueries(queryClient, tenantId);
    },
  });
}

export function useCreatePivotMutation(tenantId: string) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (payload: CreatePivotRequest) => dataModelApi.createPivot(tenantId, payload),
    onSuccess: () => {
      invalidateTableQueries(queryClient, tenantId);
    },
  });
}

export function useImportPortableDataModelMutation(tenantId: string) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (dataModel: PortableDataModelDocument) =>
      dataModelApi.importPortableDataModel(tenantId, dataModel),
    onSuccess: () => {
      invalidateTableQueries(queryClient, tenantId);
    },
  });
}
