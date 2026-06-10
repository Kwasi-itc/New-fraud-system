export type ApiErrorEnvelope = {
  error: {
    code: string;
    message: string;
  };
};

export type Tenant = {
  id: string;
  name: string;
  external_key?: string | null;
  schema_name: string;
  status: string;
  created_at: string;
  updated_at: string;
};

export type Table = {
  id: string;
  name: string;
  description: string;
  alias: string;
  semantic_type: string;
  caption_field: string;
  archived: boolean;
  created_at: string;
  updated_at: string;
};

export type CreateTableRequest = {
  name: string;
  description?: string;
  alias?: string;
  semantic_type?: string;
};

export type UpdateTableRequest = {
  description?: string;
  alias?: string;
  semantic_type?: string;
  caption_field?: string;
};

export type CreateFieldEnumValueRequest = {
  value: string;
  label: string;
  sort_order?: number;
};

export type UpdateFieldEnumValueRequest = {
  value?: string;
  label?: string;
  sort_order?: number;
};

export type CreateFieldRequest = {
  name: string;
  description?: string;
  data_type: "bool" | "int" | "float" | "string" | "timestamp" | "ip_address";
  nullable?: boolean;
  is_enum?: boolean;
  is_unique?: boolean;
  enum_values?: CreateFieldEnumValueRequest[];
};

export type UpdateFieldRequest = {
  description?: string;
  nullable?: boolean;
  is_enum?: boolean;
  is_unique?: boolean;
};

export type CreateLinkRequest = {
  name: string;
  parent_table_id: string;
  parent_field_id: string;
  child_table_id: string;
  child_field_id: string;
};

export type TableOptionsFieldDetail = {
  id: string;
  name: string;
  data_type: string;
  description: string;
  nullable: boolean;
  is_enum: boolean;
  is_unique: boolean;
};

export type TableOptions = {
  id: string;
  table_id: string;
  displayed_fields: string[];
  displayed_field_details: TableOptionsFieldDetail[];
  field_order: string[];
  field_order_details: TableOptionsFieldDetail[];
  updated_at: string;
};

export type NavigationOption = {
  id: string;
  tenant_id: string;
  source_table_id: string;
  source_field_id: string;
  target_table_id: string;
  filter_field_id: string;
  ordering_field_id: string;
  source_table_name: string;
  source_field_name: string;
  target_table_name: string;
  filter_field_name: string;
  ordering_field_name: string;
  created_at: string;
};

export type FieldEnumValue = {
  id: string;
  field_id: string;
  value: string;
  label: string;
  sort_order: number;
  created_at: string;
  updated_at: string;
};

export type Field = {
  id: string;
  name: string;
  description: string;
  data_type: string;
  nullable: boolean;
  is_enum: boolean;
  is_unique: boolean;
  archived: boolean;
  created_at: string;
  updated_at: string;
};

export type AssembledField = {
  id: string;
  name: string;
  description: string;
  data_type: string;
  nullable: boolean;
  is_enum: boolean;
  is_unique: boolean;
  archived: boolean;
  enum_values: FieldEnumValue[];
};

export type AssembledLink = {
  id: string;
  name: string;
  parent_table_id: string;
  parent_field_id: string;
  child_table_id: string;
  child_field_id: string;
  parent_table_name: string;
  child_table_name: string;
};

export type Link = {
  id: string;
  name: string;
  parent_table_id: string;
  parent_field_id: string;
  child_table_id: string;
  child_field_id: string;
  created_at: string;
};

export type AssembledTable = {
  id: string;
  name: string;
  description: string;
  alias: string;
  semantic_type: string;
  caption_field: string;
  archived: boolean;
  fields: Record<string, AssembledField>;
  links_to_single: Record<string, AssembledLink>;
  navigation_options: NavigationOption[];
  options?: TableOptions | null;
};

export type AssembledPivot = {
  id: string;
  base_table_id: string;
  field_id?: string | null;
  path_link_ids: string[];
  created_at: string;
};

export type IngestionContract = {
  tenant_status: string;
  writable: boolean;
  managed_system_fields: string[];
  record_lookup_field: string;
  partial_updates: boolean;
};

export type AssembledDataModel = {
  revision_id: string;
  ingestion_contract: IngestionContract;
  tables: Record<string, AssembledTable>;
  pivots: AssembledPivot[];
};

export type SchemaChange = {
  id: string;
  tenant_id: string;
  actor?: string | null;
  action: string;
  object_type: string;
  object_id: string;
  summary: string;
  created_at: string;
};

export type IndexJob = {
  id: string;
  tenant_id: string;
  table_id: string;
  kind: string;
  status: string;
  attempt_count: number;
  max_attempts: number;
  error_message?: string | null;
  created_at: string;
  updated_at: string;
};

const serviceBaseUrl =
  process.env.NEXT_PUBLIC_DATA_MODEL_SERVICE_URL ?? "http://localhost:8080";
const serviceToken = process.env.NEXT_PUBLIC_DATA_MODEL_SERVICE_TOKEN;

async function apiFetch<T>(path: string, init?: RequestInit): Promise<T> {
  const headers = new Headers(init?.headers);
  headers.set("Accept", "application/json");

  if (init?.body && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }

  if (serviceToken) {
    headers.set("Authorization", `Bearer ${serviceToken}`);
  }

  const response = await fetch(`${serviceBaseUrl}${path}`, {
    ...init,
    headers,
  });

  if (!response.ok) {
    const errorBody = (await response.json().catch(() => null)) as ApiErrorEnvelope | null;
    throw new Error(errorBody?.error.message ?? `Request failed with status ${response.status}`);
  }

  if (response.status === 204) {
    return undefined as T;
  }

  return (await response.json()) as T;
}

export const dataModelApi = {
  getTenant: async (tenantId: string) =>
    apiFetch<{ tenant: Tenant }>(`/v1/tenants/${tenantId}`),
  getAssembledDataModel: async (tenantId: string) =>
    apiFetch<{ data_model: AssembledDataModel }>(`/v1/tenants/${tenantId}/data-model`),
  listTables: async (tenantId: string) =>
    apiFetch<{ tables: Table[] }>(`/v1/tenants/${tenantId}/tables`),
  createTable: async (tenantId: string, payload: CreateTableRequest) =>
    apiFetch<{ table: Table }>(`/v1/tenants/${tenantId}/tables`, {
      method: "POST",
      body: JSON.stringify(payload),
    }),
  updateTable: async (tableId: string, payload: UpdateTableRequest) =>
    apiFetch<{ table: Table }>(`/v1/tables/${tableId}`, {
      method: "PATCH",
      body: JSON.stringify(payload),
    }),
  deleteTable: async (tableId: string) =>
    apiFetch<void>(`/v1/tables/${tableId}`, {
      method: "DELETE",
    }),
  createField: async (tableId: string, payload: CreateFieldRequest) =>
    apiFetch<{ field: Field }>(`/v1/tables/${tableId}/fields`, {
      method: "POST",
      body: JSON.stringify(payload),
    }),
  updateField: async (fieldId: string, payload: UpdateFieldRequest) =>
    apiFetch<{ field: Field }>(`/v1/fields/${fieldId}`, {
      method: "PATCH",
      body: JSON.stringify(payload),
    }),
  deleteField: async (fieldId: string) =>
    apiFetch<void>(`/v1/fields/${fieldId}`, {
      method: "DELETE",
    }),
  createFieldEnumValue: async (fieldId: string, payload: CreateFieldEnumValueRequest) =>
    apiFetch<{ enum_value: FieldEnumValue }>(`/v1/fields/${fieldId}/enum-values`, {
      method: "POST",
      body: JSON.stringify(payload),
    }),
  updateFieldEnumValue: async (
    enumValueId: string,
    payload: UpdateFieldEnumValueRequest
  ) =>
    apiFetch<{ enum_value: FieldEnumValue }>(`/v1/enum-values/${enumValueId}`, {
      method: "PATCH",
      body: JSON.stringify(payload),
    }),
  deleteFieldEnumValue: async (enumValueId: string) =>
    apiFetch<void>(`/v1/enum-values/${enumValueId}`, {
      method: "DELETE",
    }),
  createLink: async (tenantId: string, payload: CreateLinkRequest) =>
    apiFetch<{ link: Link }>(`/v1/tenants/${tenantId}/links`, {
      method: "POST",
      body: JSON.stringify(payload),
    }),
  listSchemaChanges: async (tenantId: string) =>
    apiFetch<{ schema_changes: SchemaChange[] }>(
      `/v1/tenants/${tenantId}/schema-change-log`
    ),
  listIndexJobs: async (tenantId: string) =>
    apiFetch<{ index_jobs: IndexJob[] }>(`/v1/tenants/${tenantId}/index-jobs`),
};
