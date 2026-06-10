"use client";

import Link from "next/link";
import { useEffect, useMemo, useState } from "react";
import { createPortal } from "react-dom";
import {
  ChevronDown,
  ChevronUp,
  Database,
  Download,
  Eye,
  FileJson2,
  MoreHorizontal,
  Pencil,
  Plus,
  Shapes,
  Trash2,
  Upload,
  X,
} from "lucide-react";
import {
  Background,
  BackgroundVariant,
  Controls,
  Handle,
  MarkerType,
  type Edge,
  type Node,
  type NodeProps,
  Position,
  ReactFlow,
  useEdgesState,
  useNodesState,
} from "@xyflow/react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  useAssembledDataModelQuery,
  useCreateFieldMutation,
  useCreateFieldEnumValueMutation,
  useCreateLinkMutation,
  useCreateTableMutation,
  useDeleteFieldEnumValueMutation,
  useDeleteFieldMutation,
  useDeleteTableMutation,
  useIndexJobsQuery,
  useSchemaChangesQuery,
  useTablesQuery,
  useTenantQuery,
  useUpdateFieldEnumValueMutation,
  useUpdateFieldMutation,
  useUpdateTableMutation,
} from "@/lib/data-model-query";
import { cn } from "@/lib/utils";
import type {
  AssembledField,
  AssembledLink,
  AssembledTable,
  Table,
} from "@/lib/data-model-api";
import {
  type DataModelView,
  useDataModelWorkspaceStore,
} from "@/stores/data-model-store";

const views: Array<{
  id: DataModelView;
  label: string;
  icon: typeof Database;
}> = [
  { id: "data-model", label: "Data model", icon: Database },
  { id: "schema", label: "Data Model Schema", icon: Shapes },
  { id: "viewer", label: "Ingested data viewer", icon: Eye },
];

type ActionCard = {
  title: string;
  description: string;
  icon: typeof Database;
  accent?: boolean;
  stat?: string;
  action?: "create-table";
};

function buildActionCards(
  view: DataModelView,
  metrics: {
    tableCount: number;
    fieldCount: number;
    schemaChangeCount: number;
    indexJobCount: number;
    writable: boolean | null;
    revisionId: string | null;
  }
): ActionCard[] {
  if (view === "schema") {
    return [
      {
        title: "Review schema changes",
        description: "Inspect the tenant schema change log and recent metadata operations",
        icon: Shapes,
        accent: true,
        stat: `${metrics.schemaChangeCount} logged changes`,
      },
      {
        title: "Validate field structure",
        description: "Compare table coverage, field counts, and model completeness",
        icon: FileJson2,
        stat: `${metrics.fieldCount} managed fields`,
      },
      {
        title: "Track published revision",
        description: "Monitor the current assembled model revision exposed to downstream systems",
        icon: Database,
        stat: metrics.revisionId ? metrics.revisionId.slice(0, 12) : "No revision yet",
      },
    ];
  }

  if (view === "viewer") {
    return [
      {
        title: "Inspect ingestion contract",
        description: "Confirm whether the tenant is writable and what system fields are managed",
        icon: Eye,
        accent: true,
        stat: metrics.writable === null ? "Unknown" : metrics.writable ? "Writable" : "Read-only",
      },
      {
        title: "Monitor index jobs",
        description: "Track managed index creation and retry operational failures",
        icon: Database,
        stat: `${metrics.indexJobCount} jobs`,
      },
      {
        title: "Prepare record viewer",
        description: "The current backend exposes model and index metadata, not row-level ingested records yet",
        icon: Upload,
        stat: "API gap",
      },
    ];
  }

  return [
    {
      title: "Select an archetype",
      description: "Choose a pre-built data model template to get started quickly",
      icon: Shapes,
      accent: true,
      stat: `${metrics.tableCount} tables available`,
    },
    {
      title: "Create a new table",
      description: "Start from scratch and define your own data model",
      icon: Database,
      stat: `${metrics.fieldCount} fields mapped`,
      action: "create-table",
    },
    {
      title: "Import from file",
      description: "Upload a previously exported organization file",
      icon: Upload,
      stat: metrics.revisionId ? "Revision published" : "Not published",
    },
  ];
}

const semanticTypes = [
  "entity",
  "event",
  "reference",
  "fact",
  "finance",
  "customer",
  "merchant",
  "transaction",
  "case",
  "risk",
  "compliance",
  "audit",
] as const;

type LocalEnumValue = {
  id?: string;
  value: string;
  label: string;
};

type SchemaNodeData = {
  tableId: string;
  name: string;
  description: string;
  hasIncomingLink: boolean;
  hasOutgoingLink: boolean;
  isCollapsed: boolean;
  fields: Array<{
    id: string;
    name: string;
    dataType: string;
    description: string;
  }>;
};

function SchemaTableNode({
  data,
  onToggleCollapse,
  onEditTable,
  onCreateField,
  onCreateLink,
}: NodeProps<Node<SchemaNodeData>> & {
  onToggleCollapse: (tableId: string) => void;
  onEditTable: (tableId: string) => void;
  onCreateField: (tableId: string) => void;
  onCreateLink: (tableId: string) => void;
}) {
  const [isMenuOpen, setIsMenuOpen] = useState(false);
  const visibleFields = data.isCollapsed ? data.fields.slice(0, 3) : data.fields;
  const hiddenFieldCount = Math.max(0, data.fields.length - visibleFields.length);

  return (
    <div className="relative w-[300px] overflow-visible rounded-xl border border-slate-200 bg-white shadow-[0_10px_24px_rgba(15,23,42,0.08)]">
      {data.hasIncomingLink ? (
        <Handle
          type="target"
          position={Position.Left}
          className="!size-2.5 !border-2 !border-white !bg-[#2563eb]"
        />
      ) : null}
      {data.hasOutgoingLink ? (
        <Handle
          type="source"
          position={Position.Right}
          className="!size-2.5 !border-2 !border-white !bg-[#2563eb]"
        />
      ) : null}

      <div className="flex items-start justify-between gap-3 border-b border-slate-200 px-4 py-3">
        <div className="min-w-0">
          <p className="truncate text-[1.05rem] font-semibold tracking-tight text-slate-950">
            {data.name}
          </p>
          <p className="mt-1 line-clamp-2 text-[11px] leading-4 text-slate-500">
            {data.description || "No description"}
          </p>
        </div>
        <div className="flex items-center gap-1">
          <button
            type="button"
            onClick={() => onToggleCollapse(data.tableId)}
            className="inline-flex size-6 items-center justify-center rounded-md border border-slate-200 bg-white text-slate-500"
            aria-label={data.isCollapsed ? `Expand ${data.name}` : `Collapse ${data.name}`}
          >
            {data.isCollapsed ? (
              <ChevronDown className="size-3.5" />
            ) : (
              <ChevronUp className="size-3.5" />
            )}
          </button>
          <button
            type="button"
            onClick={() => setIsMenuOpen((current) => !current)}
            className="inline-flex size-6 items-center justify-center rounded-md bg-[#1d4ed8] text-white"
            aria-label={`Open ${data.name} options`}
          >
            <MoreHorizontal className="size-3.5" />
          </button>
        </div>
      </div>

      {isMenuOpen ? (
        <div className="absolute right-4 top-11 z-20 w-52 overflow-hidden rounded-xl border border-slate-200 bg-white shadow-[0_16px_38px_rgba(15,23,42,0.12)]">
          <button
            type="button"
            onClick={() => {
              setIsMenuOpen(false);
              onEditTable(data.tableId);
            }}
            className="flex w-full items-center gap-3 px-4 py-3 text-left text-sm text-slate-900 transition-colors hover:bg-slate-50"
          >
            <Pencil className="size-4 text-[#2563eb]" />
            Edit table
          </button>
          <button
            type="button"
            onClick={() => {
              setIsMenuOpen(false);
              onCreateField(data.tableId);
            }}
            className="flex w-full items-center gap-3 px-4 py-3 text-left text-sm text-slate-900 transition-colors hover:bg-slate-50"
          >
            <Plus className="size-4 text-[#2563eb]" />
            Create a new field
          </button>
          <button
            type="button"
            onClick={() => {
              setIsMenuOpen(false);
              onCreateLink(data.tableId);
            }}
            className="flex w-full items-center gap-3 px-4 py-3 text-left text-sm text-slate-900 transition-colors hover:bg-slate-50"
          >
            <Plus className="size-4 text-[#2563eb]" />
            Create a new link
          </button>
        </div>
      ) : null}

      <table className="w-full border-collapse text-left">
        <thead className="bg-slate-50/80">
          <tr className="border-b border-slate-200 text-[11px] uppercase tracking-[0.06em] text-slate-500">
            <th className="px-4 py-2.5 font-medium">Name</th>
            <th className="px-4 py-2.5 font-medium">Type</th>
            <th className="px-4 py-2.5 font-medium">Description</th>
          </tr>
        </thead>
        <tbody>
          {visibleFields.length > 0 ? (
            visibleFields.map((field, index) => (
              <tr
                key={field.id}
                className={cn(
                  "border-b border-slate-100 text-[11px] text-slate-900 last:border-b-0",
                  index % 2 === 1 && "bg-slate-50/60"
                )}
              >
                <td className="px-4 py-2.5 font-medium">{field.name}</td>
                <td className="px-4 py-2.5 text-slate-600">{field.dataType}</td>
                <td className="px-4 py-2.5 text-slate-600">{field.description || " "}</td>
              </tr>
            ))
          ) : (
            <tr>
              <td className="px-4 py-4 text-[11px] text-slate-500" colSpan={3}>
                No fields defined yet.
              </td>
            </tr>
          )}
          {data.isCollapsed && hiddenFieldCount > 0 ? (
            <tr>
              <td className="px-4 py-3 text-[11px] font-medium text-[#2563eb]" colSpan={3}>
                {hiddenFieldCount} more field{hiddenFieldCount === 1 ? "" : "s"}
              </td>
            </tr>
          ) : null}
        </tbody>
      </table>
    </div>
  );
}

function buildSchemaElements(
  tables: AssembledTable[],
  options: {
    collapsedTableIds: Set<string>;
  }
) {
  const sortedTables = [...tables].sort((a, b) => a.name.localeCompare(b.name));
  const columns = Math.max(1, Math.ceil(Math.sqrt(sortedTables.length || 1)));
  const xSpacing = 430;
  const ySpacing = 260;
  const links = sortedTables.flatMap((table) => Object.values(table.links_to_single));
  const incomingTableIds = new Set(links.map((link) => link.parent_table_id));
  const outgoingTableIds = new Set(links.map((link) => link.child_table_id));

  const nodes: Node<SchemaNodeData>[] = sortedTables.map((table, index) => {
    const column = index % columns;
    const row = Math.floor(index / columns);
    const fields = Object.values(table.fields)
      .sort((a, b) => a.name.localeCompare(b.name))
      .slice(0, 6)
      .map((field) => ({
        id: field.id,
        name: field.name,
        dataType: field.data_type,
        description: field.description,
      }));

    return {
      id: table.id,
      type: "schemaTable",
      position: {
        x: column * xSpacing,
        y: row * ySpacing + (column % 2 === 1 ? 48 : 0),
      },
      data: {
        tableId: table.id,
        name: table.name,
        description: table.description,
        hasIncomingLink: incomingTableIds.has(table.id),
        hasOutgoingLink: outgoingTableIds.has(table.id),
        isCollapsed: options.collapsedTableIds.has(table.id),
        fields,
      },
    };
  });

  const edges: Edge[] = links.map((link: AssembledLink) => ({
    id: link.id,
    source: link.child_table_id,
    target: link.parent_table_id,
    type: "smoothstep",
    label: link.name,
    labelStyle: {
      fill: "#334155",
      fontSize: 11,
      fontWeight: 600,
    },
    labelBgStyle: {
      fill: "#ffffff",
      fillOpacity: 0.92,
    },
    labelBgPadding: [6, 3],
    labelBgBorderRadius: 6,
    markerEnd: {
      type: MarkerType.ArrowClosed,
      width: 18,
      height: 18,
      color: "#94a3b8",
    },
    style: {
      stroke: "#94a3b8",
      strokeWidth: 1.5,
    },
  }));

  return { nodes, edges };
}

export default function YourDataPage() {
  const tenantId = useDataModelWorkspaceStore((state) => state.tenantId);
  const activeView = useDataModelWorkspaceStore((state) => state.activeView);
  const setActiveView = useDataModelWorkspaceStore((state) => state.setActiveView);

  const [isCreateModalOpen, setIsCreateModalOpen] = useState(false);
  const [isCreateFieldModalOpen, setIsCreateFieldModalOpen] = useState(false);
  const [isCreateLinkModalOpen, setIsCreateLinkModalOpen] = useState(false);
  const [isEditFieldModalOpen, setIsEditFieldModalOpen] = useState(false);
  const [isDeleteModalOpen, setIsDeleteModalOpen] = useState(false);
  const [isDeleteFieldModalOpen, setIsDeleteFieldModalOpen] = useState(false);
  const [editingTable, setEditingTable] = useState<Table | null>(null);
  const [pendingDeleteTable, setPendingDeleteTable] = useState<Table | null>(null);
  const [fieldTable, setFieldTable] = useState<Table | null>(null);
  const [editingField, setEditingField] = useState<AssembledField | null>(null);
  const [pendingDeleteField, setPendingDeleteField] = useState<AssembledField | null>(null);
  const [openTableMenuId, setOpenTableMenuId] = useState<string | null>(null);
  const [expandedTableIds, setExpandedTableIds] = useState<string[]>([]);
  const [name, setName] = useState("");
  const [alias, setAlias] = useState("");
  const [description, setDescription] = useState("");
  const [semanticType, setSemanticType] =
    useState<(typeof semanticTypes)[number]>("entity");
  const [formError, setFormError] = useState<string | null>(null);
  const [fieldName, setFieldName] = useState("");
  const [fieldDescription, setFieldDescription] = useState("");
  const [fieldIsRequired, setFieldIsRequired] = useState(false);
  const [fieldType, setFieldType] =
    useState<"bool" | "int" | "float" | "string" | "timestamp" | "ip_address">("string");
  const [fieldIsEnum, setFieldIsEnum] = useState(false);
  const [fieldIsUnique, setFieldIsUnique] = useState(false);
  const [fieldEnumValues, setFieldEnumValues] = useState<LocalEnumValue[]>([]);
  const [fieldFormError, setFieldFormError] = useState<string | null>(null);
  const [linkName, setLinkName] = useState("");
  const [linkChildTableId, setLinkChildTableId] = useState("");
  const [linkChildFieldId, setLinkChildFieldId] = useState("");
  const [linkParentTableId, setLinkParentTableId] = useState("");
  const [linkParentFieldId, setLinkParentFieldId] = useState("");
  const [linkFormError, setLinkFormError] = useState<string | null>(null);
  const [viewerTableId, setViewerTableId] = useState("");
  const [viewerObjectId, setViewerObjectId] = useState("");
  const [viewerSearchMessage, setViewerSearchMessage] = useState<string | null>(null);
  const [schemaCollapsedTableIds, setSchemaCollapsedTableIds] = useState<string[]>([]);

  const tenantQuery = useTenantQuery(tenantId);
  const assembledModelQuery = useAssembledDataModelQuery(tenantId);
  const tablesQuery = useTablesQuery(tenantId);
  const schemaChangesQuery = useSchemaChangesQuery(tenantId);
  const indexJobsQuery = useIndexJobsQuery(tenantId);
  const createTableMutation = useCreateTableMutation(tenantId);
  const createFieldMutation = useCreateFieldMutation(tenantId);
  const updateTableMutation = useUpdateTableMutation(tenantId);
  const deleteTableMutation = useDeleteTableMutation(tenantId);
  const updateFieldMutation = useUpdateFieldMutation(tenantId);
  const deleteFieldMutation = useDeleteFieldMutation(tenantId);
  const createFieldEnumValueMutation = useCreateFieldEnumValueMutation(tenantId);
  const updateFieldEnumValueMutation = useUpdateFieldEnumValueMutation(tenantId);
  const deleteFieldEnumValueMutation = useDeleteFieldEnumValueMutation(tenantId);
  const createLinkMutation = useCreateLinkMutation(tenantId);

  const assembledModelTables = assembledModelQuery.data?.data_model.tables;
  const assembledTables = Object.values(assembledModelTables ?? {});
  const tableCount = tablesQuery.data?.tables.length ?? assembledTables.length;
  const fieldCount = assembledTables.reduce(
    (count, table) => count + Object.keys(table.fields).length,
    0
  );
  const schemaChangeCount = schemaChangesQuery.data?.schema_changes.length ?? 0;
  const indexJobCount = indexJobsQuery.data?.index_jobs.length ?? 0;
  const writable = assembledModelQuery.data?.data_model.ingestion_contract.writable ?? null;
  const revisionId = assembledModelQuery.data?.data_model.revision_id ?? null;

  const actionCards = buildActionCards(activeView, {
    tableCount,
    fieldCount,
    schemaChangeCount,
    indexJobCount,
    writable,
    revisionId,
  });

  const isLoading =
    tenantQuery.isLoading ||
    assembledModelQuery.isLoading ||
    tablesQuery.isLoading ||
    schemaChangesQuery.isLoading ||
    indexJobsQuery.isLoading;

  const error =
    tenantQuery.error ??
    assembledModelQuery.error ??
    tablesQuery.error ??
    schemaChangesQuery.error ??
    indexJobsQuery.error;

  const sortedTables = useMemo(
    () =>
      [...(tablesQuery.data?.tables ?? [])].sort((a, b) =>
        a.name.localeCompare(b.name)
      ),
    [tablesQuery.data?.tables]
  );
  const showTableListMode = activeView === "data-model" && sortedTables.length > 0;
  const getAssembledTableById = (tableId: string) =>
    assembledTables.find((table) => table.id === tableId);
  const childTable = linkChildTableId ? getAssembledTableById(linkChildTableId) : undefined;
  const parentTable = linkParentTableId ? getAssembledTableById(linkParentTableId) : undefined;
  const childFieldOptions = childTable
    ? Object.values(childTable.fields).sort((a, b) => a.name.localeCompare(b.name))
    : [];
  const parentFieldOptions = parentTable
    ? Object.values(parentTable.fields).sort((a, b) => a.name.localeCompare(b.name))
    : [];

  function toggleExpandedTable(tableId: string) {
    setExpandedTableIds((current) =>
      current.includes(tableId)
        ? current.filter((id) => id !== tableId)
        : [...current, tableId]
    );
  }

  function toggleTableMenu(tableId: string) {
    setOpenTableMenuId((current) => (current === tableId ? null : tableId));
  }

  function formatFieldType(dataType: string) {
    switch (dataType) {
      case "bool":
        return "Boolean";
      case "int":
        return "Integer";
      case "float":
        return "Float";
      case "string":
        return "String";
      case "timestamp":
        return "Timestamp";
      case "ip_address":
        return "IP address";
      default:
        return dataType;
    }
  }

  function getTableNameById(tableId: string) {
    return getAssembledTableById(tableId)?.name ?? "Unknown table";
  }

  function getFieldNameById(tableId: string, fieldId: string) {
    const assembledTable = getAssembledTableById(tableId);
    if (!assembledTable) {
      return fieldId;
    }

    const field = Object.values(assembledTable.fields).find((item) => item.id === fieldId);
    return field?.name ?? fieldId;
  }

  function resetCreateTableForm() {
    setName("");
    setAlias("");
    setDescription("");
    setSemanticType("entity");
    setFormError(null);
    setEditingTable(null);
  }

  function openCreateModal() {
    resetCreateTableForm();
    setIsCreateModalOpen(true);
  }

  function openEditModal(table: Table) {
    setEditingTable(table);
    setName(table.name);
    setAlias(table.alias || "");
    setDescription(table.description || "");
    setSemanticType(
      semanticTypes.includes(table.semantic_type as (typeof semanticTypes)[number])
        ? (table.semantic_type as (typeof semanticTypes)[number])
        : "entity"
    );
    setFormError(null);
    setIsCreateModalOpen(true);
  }

  function closeCreateModal() {
    if (createTableMutation.isPending || updateTableMutation.isPending) {
      return;
    }
    setIsCreateModalOpen(false);
    setFormError(null);
    setEditingTable(null);
  }

  function openDeleteModal(table: Table) {
    setPendingDeleteTable(table);
    setFormError(null);
    setIsDeleteModalOpen(true);
  }

  function resetCreateFieldForm() {
    setFieldName("");
    setFieldDescription("");
    setFieldIsRequired(false);
    setFieldType("string");
    setFieldIsEnum(false);
    setFieldIsUnique(false);
    setFieldEnumValues([]);
    setFieldFormError(null);
  }

  function openCreateFieldModal(table: Table) {
    resetCreateFieldForm();
    setFieldTable(table);
    setEditingField(null);
    setIsCreateFieldModalOpen(true);
  }

  function openEditFieldModal(table: Table, field: AssembledField) {
    setFieldTable(table);
    setEditingField(field);
    setFieldName(field.name);
    setFieldDescription(field.description || "");
    setFieldIsRequired(!field.nullable);
    setFieldType(
      field.data_type as "bool" | "int" | "float" | "string" | "timestamp" | "ip_address"
    );
    setFieldIsEnum(field.is_enum);
    setFieldIsUnique(field.is_unique);
    setFieldEnumValues(
      field.enum_values.map((enumValue) => ({
        id: enumValue.id,
        value: enumValue.value,
        label: enumValue.label,
      }))
    );
    setFieldFormError(null);
    setIsEditFieldModalOpen(true);
  }

  function closeCreateFieldModal() {
    if (createFieldMutation.isPending) {
      return;
    }
    setIsCreateFieldModalOpen(false);
    setFieldTable(null);
    setFieldFormError(null);
  }

  function closeEditFieldModal() {
    if (updateFieldMutation.isPending) {
      return;
    }
    setIsEditFieldModalOpen(false);
    setFieldTable(null);
    setEditingField(null);
    setFieldFormError(null);
  }

  function closeDeleteModal() {
    if (deleteTableMutation.isPending) {
      return;
    }
    setIsDeleteModalOpen(false);
    setPendingDeleteTable(null);
  }

  function openDeleteFieldModal(table: Table, field: AssembledField) {
    setFieldTable(table);
    setPendingDeleteField(field);
    setFieldFormError(null);
    setIsDeleteFieldModalOpen(true);
  }

  function closeDeleteFieldModal() {
    if (deleteFieldMutation.isPending) {
      return;
    }
    setIsDeleteFieldModalOpen(false);
    setFieldTable(null);
    setPendingDeleteField(null);
    setFieldFormError(null);
  }

  function resetCreateLinkForm() {
    setLinkName("");
    setLinkChildTableId("");
    setLinkChildFieldId("");
    setLinkParentTableId("");
    setLinkParentFieldId("");
    setLinkFormError(null);
  }

  function openCreateLinkModal(table: Table) {
    const assembledTable = getAssembledTableById(table.id);
    const sortedFields = assembledTable
      ? Object.values(assembledTable.fields).sort((a, b) => a.name.localeCompare(b.name))
      : [];
    const defaultChildField = sortedFields[0]?.id ?? "";
    const defaultParentTable = assembledTables.find(
      (candidate) =>
        candidate.id !== table.id &&
        Object.values(candidate.fields).some((field) => field.is_unique)
    );
    const defaultParentField = defaultParentTable
      ? Object.values(defaultParentTable.fields)
          .sort((a, b) => a.name.localeCompare(b.name))[0]?.id ?? ""
      : "";

    setLinkName("");
    setLinkChildTableId(table.id);
    setLinkChildFieldId(defaultChildField);
    setLinkParentTableId(defaultParentTable?.id ?? "");
    setLinkParentFieldId(defaultParentField);
    setLinkFormError(null);
    setIsCreateLinkModalOpen(true);
  }

  function closeCreateLinkModal() {
    if (createLinkMutation.isPending) {
      return;
    }
    setIsCreateLinkModalOpen(false);
    resetCreateLinkForm();
  }

  function handleSchemaToggleCollapse(tableId: string) {
    setSchemaCollapsedTableIds((current) =>
      current.includes(tableId)
        ? current.filter((id) => id !== tableId)
        : [...current, tableId]
    );
  }

  function handleSchemaEditTable(tableId: string) {
    const table = (tablesQuery.data?.tables ?? []).find((item) => item.id === tableId);
    if (!table) {
      return;
    }

    setEditingTable(table);
    setName(table.name);
    setAlias(table.alias || "");
    setDescription(table.description || "");
    setSemanticType(
      semanticTypes.includes(table.semantic_type as (typeof semanticTypes)[number])
        ? (table.semantic_type as (typeof semanticTypes)[number])
        : "entity"
    );
    setFormError(null);
    setIsCreateModalOpen(true);
  }

  function handleSchemaCreateField(tableId: string) {
    const table = (tablesQuery.data?.tables ?? []).find((item) => item.id === tableId);
    if (!table) {
      return;
    }

    resetCreateFieldForm();
    setFieldTable(table);
    setEditingField(null);
    setIsCreateFieldModalOpen(true);
  }

  function handleSchemaCreateLink(tableId: string) {
    const table = (tablesQuery.data?.tables ?? []).find((item) => item.id === tableId);
    if (!table) {
      return;
    }

    const assembledTable = assembledTables.find((item) => item.id === table.id);
    const sortedFields = assembledTable
      ? Object.values(assembledTable.fields).sort((a, b) => a.name.localeCompare(b.name))
      : [];
    const defaultChildField = sortedFields[0]?.id ?? "";
    const defaultParentTable = assembledTables.find(
      (candidate) =>
        candidate.id !== table.id &&
        Object.values(candidate.fields).some((field) => field.is_unique)
    );
    const defaultParentField = defaultParentTable
      ? Object.values(defaultParentTable.fields)
          .sort((a, b) => a.name.localeCompare(b.name))[0]?.id ?? ""
      : "";

    setLinkName("");
    setLinkChildTableId(table.id);
    setLinkChildFieldId(defaultChildField);
    setLinkParentTableId(defaultParentTable?.id ?? "");
    setLinkParentFieldId(defaultParentField);
    setLinkFormError(null);
    setIsCreateLinkModalOpen(true);
  }

  const schemaNodeTypes = {
    schemaTable: (props: NodeProps<Node<SchemaNodeData>>) => (
      <SchemaTableNode
        {...props}
        onToggleCollapse={handleSchemaToggleCollapse}
        onEditTable={handleSchemaEditTable}
        onCreateField={handleSchemaCreateField}
        onCreateLink={handleSchemaCreateLink}
      />
    ),
  };

  const initialSchemaElements = buildSchemaElements(assembledTables, {
    collapsedTableIds: new Set(schemaCollapsedTableIds),
  });
  const [schemaNodes, setSchemaNodes, onSchemaNodesChange] = useNodesState(
    initialSchemaElements.nodes
  );
  const [schemaEdges, setSchemaEdges, onSchemaEdgesChange] = useEdgesState(
    initialSchemaElements.edges
  );

  useEffect(() => {
    const nextSchemaElements = buildSchemaElements(
      Object.values(assembledModelTables ?? {}),
      { collapsedTableIds: new Set(schemaCollapsedTableIds) }
    );
    setSchemaNodes((currentNodes) =>
      nextSchemaElements.nodes.map((nextNode) => {
        const currentNode = currentNodes.find((node) => node.id === nextNode.id);
        return currentNode
          ? { ...nextNode, position: currentNode.position }
          : nextNode;
      })
    );
    setSchemaEdges(nextSchemaElements.edges);
  }, [
    assembledModelTables,
    revisionId,
    schemaCollapsedTableIds,
    setSchemaEdges,
    setSchemaNodes,
  ]);

  function addEnumValueRow() {
    setFieldEnumValues((current) => [...current, { value: "", label: "" }]);
  }

  function updateEnumValueRow(
    index: number,
    key: keyof Pick<LocalEnumValue, "value" | "label">,
    nextValue: string
  ) {
    setFieldEnumValues((current) =>
      current.map((item, itemIndex) =>
        itemIndex === index ? { ...item, [key]: nextValue } : item
      )
    );
  }

  function removeEnumValueRow(index: number) {
    setFieldEnumValues((current) => current.filter((_, itemIndex) => itemIndex !== index));
  }

  function handleLinkParentTableChange(nextParentTableId: string) {
    setLinkParentTableId(nextParentTableId);
    const nextParentTable = getAssembledTableById(nextParentTableId);
    const nextParentField = nextParentTable
      ? Object.values(nextParentTable.fields)
          .sort((a, b) => a.name.localeCompare(b.name))[0]?.id ?? ""
      : "";
    setLinkParentFieldId(nextParentField);
  }

  async function handleCreateTableSubmit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setFormError(null);

    if (!name.trim()) {
      setFormError("Table name is required.");
      return;
    }

    try {
      if (editingTable) {
        await updateTableMutation.mutateAsync({
          tableId: editingTable.id,
          payload: {
            alias: alias.trim() || undefined,
            description: description.trim() || undefined,
            semantic_type: semanticType,
          },
        });
      } else {
        await createTableMutation.mutateAsync({
          name: name.trim(),
          alias: alias.trim() || undefined,
          description: description.trim() || undefined,
          semantic_type: semanticType,
        });
      }
      closeCreateModal();
      resetCreateTableForm();
    } catch (mutationError) {
      setFormError(
        mutationError instanceof Error
          ? mutationError.message
          : "Failed to create table."
      );
    }
  }

  async function handleDeleteTable() {
    if (!pendingDeleteTable) {
      return;
    }

    setFormError(null);

    try {
      await deleteTableMutation.mutateAsync(pendingDeleteTable.id);
      closeDeleteModal();
    } catch (mutationError) {
      setFormError(
        mutationError instanceof Error
          ? mutationError.message
          : "Failed to delete table."
      );
    }
  }

  async function handleCreateFieldSubmit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setFieldFormError(null);

    if (!fieldTable) {
      setFieldFormError("No table selected for field creation.");
      return;
    }

    if (!fieldName.trim()) {
      setFieldFormError("Field name is required.");
      return;
    }

    if (fieldIsEnum) {
      if (fieldEnumValues.length === 0) {
        setFieldFormError("Add at least one enum value for enumerated fields.");
        return;
      }

      const hasInvalidEnumValue = fieldEnumValues.some(
        (item) => !item.value.trim() || !item.label.trim()
      );

      if (hasInvalidEnumValue) {
        setFieldFormError("Each enum value needs both a value and a label.");
        return;
      }
    }

    try {
      await createFieldMutation.mutateAsync({
        tableId: fieldTable.id,
        payload: {
          name: fieldName.trim(),
          description: fieldDescription.trim() || undefined,
          data_type: fieldType,
          nullable: !fieldIsRequired,
          is_enum: fieldIsEnum,
          is_unique: fieldIsUnique,
          enum_values: fieldIsEnum
            ? fieldEnumValues.map((item, index) => ({
                value: item.value.trim(),
                label: item.label.trim(),
                sort_order: index + 1,
              }))
            : undefined,
        },
      });
      closeCreateFieldModal();
      resetCreateFieldForm();
    } catch (mutationError) {
      setFieldFormError(
        mutationError instanceof Error
          ? mutationError.message
          : "Failed to create field."
      );
    }
  }

  async function handleEditFieldSubmit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setFieldFormError(null);

    if (!editingField) {
      setFieldFormError("No field selected for editing.");
      return;
    }

    if (fieldIsEnum) {
      if (fieldEnumValues.length === 0) {
        setFieldFormError("Add at least one enum value for enumerated fields.");
        return;
      }

      const hasInvalidEnumValue = fieldEnumValues.some(
        (item) => !item.value.trim() || !item.label.trim()
      );

      if (hasInvalidEnumValue) {
        setFieldFormError("Each enum value needs both a value and a label.");
        return;
      }
    }

    try {
      await updateFieldMutation.mutateAsync({
        fieldId: editingField.id,
        payload: {
          description: fieldDescription.trim() || undefined,
          nullable: !fieldIsRequired,
          is_enum: fieldIsEnum,
          is_unique: fieldIsUnique,
        },
      });

      const originalEnumValues = editingField.enum_values;
      const originalById = new Map(originalEnumValues.map((item) => [item.id, item]));

      const deletedEnumValues = originalEnumValues.filter(
        (original) => !fieldEnumValues.some((current) => current.id === original.id)
      );

      for (const deletedEnumValue of deletedEnumValues) {
        await deleteFieldEnumValueMutation.mutateAsync(deletedEnumValue.id);
      }

      if (fieldIsEnum) {
        for (const [index, item] of fieldEnumValues.entries()) {
          if (!item.id) {
            await createFieldEnumValueMutation.mutateAsync({
              fieldId: editingField.id,
              payload: {
                value: item.value.trim(),
                label: item.label.trim(),
                sort_order: index + 1,
              },
            });
            continue;
          }

          const original = originalById.get(item.id);
          if (
            original &&
            (original.value !== item.value.trim() ||
              original.label !== item.label.trim() ||
              original.sort_order !== index + 1)
          ) {
            await updateFieldEnumValueMutation.mutateAsync({
              enumValueId: item.id,
              payload: {
                value: item.value.trim(),
                label: item.label.trim(),
                sort_order: index + 1,
              },
            });
          }
        }
      }

      closeEditFieldModal();
      resetCreateFieldForm();
    } catch (mutationError) {
      setFieldFormError(
        mutationError instanceof Error
          ? mutationError.message
          : "Failed to update field."
      );
    }
  }

  async function handleDeleteField() {
    if (!pendingDeleteField) {
      return;
    }

    setFieldFormError(null);

    try {
      await deleteFieldMutation.mutateAsync(pendingDeleteField.id);
      closeDeleteFieldModal();
    } catch (mutationError) {
      setFieldFormError(
        mutationError instanceof Error
          ? mutationError.message
          : "Failed to delete field."
      );
    }
  }

  async function handleCreateLinkSubmit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setLinkFormError(null);

    if (!linkName.trim()) {
      setLinkFormError("Link name is required.");
      return;
    }

    if (!linkChildTableId || !linkChildFieldId || !linkParentTableId || !linkParentFieldId) {
      setLinkFormError("Select both sides of the link before continuing.");
      return;
    }

    const selectedParentField = parentFieldOptions.find((field) => field.id === linkParentFieldId);
    if (!selectedParentField) {
      setLinkFormError("Select a target field before continuing.");
      return;
    }

    if (!selectedParentField.is_unique) {
      setLinkFormError("The target field must be marked as unique before it can be linked.");
      return;
    }

    try {
      await createLinkMutation.mutateAsync({
        name: linkName.trim(),
        child_table_id: linkChildTableId,
        child_field_id: linkChildFieldId,
        parent_table_id: linkParentTableId,
        parent_field_id: linkParentFieldId,
      });
      closeCreateLinkModal();
    } catch (mutationError) {
      setLinkFormError(
        mutationError instanceof Error ? mutationError.message : "Failed to create link."
      );
    }
  }

  if (!tenantId) {
    return (
      <div className="space-y-4 rounded-xl border border-dashed border-slate-300 bg-slate-50 p-6">
        <h2 className="text-2xl font-semibold tracking-tight text-slate-950">
          Your Data Model
        </h2>
        <p className="max-w-2xl text-sm leading-7 text-slate-600">
          Set `NEXT_PUBLIC_DATA_MODEL_TENANT_ID` to connect this workspace to a real
          tenant in the data-model-service. The frontend query foundation is in place,
          but it needs a tenant id before it can fetch tables, schema history, and
          index jobs.
        </p>
      </div>
    );
  }

  return (
    <>
      <div className="space-y-9">
        <section className="space-y-4">
          <div className="flex flex-col gap-3 lg:flex-row lg:items-end lg:justify-between">
            <div>
              <h2 className="text-3xl font-semibold tracking-tight text-slate-950">
                Your Data Model
              </h2>
              <p className="mt-2 text-sm leading-7 text-slate-600">
                {tenantQuery.data?.tenant.name ?? "Tenant workspace"} mapped through the
                data-model-service contract.
              </p>
            </div>
            <div className="flex flex-wrap gap-3 text-sm text-slate-600">
              <div className="rounded-xl border border-slate-200 bg-white px-4 py-2">
                Status:{" "}
                <span className="font-semibold text-slate-950">
                  {tenantQuery.data?.tenant.status ?? "Unknown"}
                </span>
              </div>
            </div>
          </div>

          <div className="inline-flex flex-wrap items-center gap-1 rounded-xl bg-[#eff6ff] p-1">
            {views.map((view) => {
              const Icon = view.icon;
              const isActive = activeView === view.id;

              return (
                <button
                  key={view.id}
                  type="button"
                  onClick={() => setActiveView(view.id)}
                  className={cn(
                    "inline-flex items-center gap-2 rounded-lg px-3 py-2 text-sm font-medium transition-colors",
                    isActive
                      ? "bg-[#2563eb] text-white shadow-sm"
                      : "text-[#2563eb] hover:bg-white/90"
                  )}
                >
                  <Icon className="size-4" />
                  {view.label}
                </button>
              );
            })}
          </div>
        </section>

        {error ? (
          <section className="rounded-xl border border-red-200 bg-red-50 p-5 text-sm text-red-700">
            {(error as Error).message}
          </section>
        ) : null}

        {!showTableListMode && activeView !== "viewer" && activeView !== "schema" ? (
          <>
            <section className="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
              {[
                { label: "Tables", value: tableCount },
                { label: "Fields", value: fieldCount },
                { label: "Schema changes", value: schemaChangeCount },
                { label: "Index jobs", value: indexJobCount },
              ].map((item) => (
                <div key={item.label} className="rounded-xl border border-slate-200 bg-white p-5">
                  <p className="text-sm text-slate-500">{item.label}</p>
                  <p className="mt-3 text-3xl font-semibold tracking-tight text-slate-950">
                    {isLoading ? "..." : item.value}
                  </p>
                </div>
              ))}
            </section>

            <section className="grid gap-4 xl:grid-cols-3">
              {actionCards.map((action) => {
                const Icon = action.icon;

                return (
                  <button
                    key={action.title}
                    type="button"
                    onClick={action.action === "create-table" ? openCreateModal : undefined}
                    className={cn(
                      "flex min-h-[190px] flex-col items-center justify-center rounded-xl border border-dashed bg-white px-8 py-10 text-center transition-colors",
                      action.accent
                        ? "border-[#93c5fd] hover:bg-[#f8fbff]"
                        : "border-slate-200 hover:bg-slate-50"
                    )}
                  >
                    <Icon className="mb-5 size-8 text-[#2563eb]" />
                    <h3 className="text-[1.05rem] font-semibold tracking-tight text-slate-950">
                      {action.title}
                    </h3>
                    <p className="mt-3 max-w-[260px] text-sm leading-7 text-slate-500">
                      {action.description}
                    </p>
                    <p className="mt-5 text-sm font-medium text-[#2563eb]">{action.stat}</p>
                  </button>
                );
              })}
            </section>
          </>
        ) : null}

        {activeView === "schema" ? (
          <section className="space-y-4">
            <div className="relative h-[72vh] overflow-hidden rounded-2xl border border-slate-200 bg-white/80 shadow-[0_12px_40px_rgba(15,23,42,0.06)]">
              <ReactFlow
                nodes={schemaNodes}
                edges={schemaEdges}
                onNodesChange={onSchemaNodesChange}
                onEdgesChange={onSchemaEdgesChange}
                nodeTypes={schemaNodeTypes}
                fitView
                fitViewOptions={{ padding: 0.18 }}
                minZoom={0.35}
                maxZoom={1.4}
                proOptions={{ hideAttribution: true }}
                defaultEdgeOptions={{
                  type: "smoothstep",
                  style: { stroke: "#94a3b8", strokeWidth: 1.5 },
                }}
              >
                <Background
                  variant={BackgroundVariant.Dots}
                  gap={20}
                  size={1}
                  color="#d7e1ef"
                />
                <Controls
                  showInteractive={false}
                  className="!overflow-hidden !rounded-xl !border !border-slate-200 !bg-white !shadow-[0_12px_24px_rgba(15,23,42,0.08)]"
                />
              </ReactFlow>

              <div className="pointer-events-none absolute inset-x-0 bottom-0 h-24 bg-gradient-to-t from-white/70 to-transparent" />

              <div className="absolute bottom-5 right-5">
                <Button
                  variant="default"
                  onClick={openCreateModal}
                  type="button"
                  className="h-11 rounded-xl bg-[#2563eb] px-5 text-sm text-white shadow-none hover:translate-y-0 hover:bg-[#1d4ed8]"
                >
                  <Plus className="size-4" />
                  Create a new table
                </Button>
              </div>
            </div>
          </section>
        ) : null}

        {activeView === "viewer" ? (
          <section className="space-y-6">
            <div className="max-w-4xl rounded-xl bg-white/0">
              <form
                className="flex flex-col gap-4 lg:flex-row lg:items-end"
                onSubmit={(event) => {
                  event.preventDefault();

                  if (!viewerTableId) {
                    setViewerSearchMessage("Select an object type before searching.");
                    return;
                  }

                  if (!viewerObjectId.trim()) {
                    setViewerSearchMessage("Enter an object id before searching.");
                    return;
                  }

                  setViewerSearchMessage(
                    "The data-model service exposes model metadata, but it does not yet provide row-level ingested record lookup for this viewer."
                  );
                }}
              >
                <div className="grid gap-4 lg:grid-cols-[200px_minmax(0,480px)_auto]">
                  <div className="space-y-2">
                    <label
                      className="text-[15px] font-medium text-slate-900"
                      htmlFor="viewer-table"
                    >
                      Object type
                    </label>
                    <select
                      id="viewer-table"
                      value={viewerTableId}
                      onChange={(event) => setViewerTableId(event.target.value)}
                      className="flex h-12 w-full rounded-lg border border-slate-200 bg-white px-3 text-base text-slate-900 outline-none focus:border-[#2563eb] focus:ring-[3px] focus:ring-blue-100"
                    >
                      <option value="">select a table</option>
                      {sortedTables.map((table) => (
                        <option key={table.id} value={table.id}>
                          {table.name}
                        </option>
                      ))}
                    </select>
                  </div>

                  <div className="space-y-2">
                    <label
                      className="text-[15px] font-medium text-slate-900"
                      htmlFor="viewer-object-id"
                    >
                      Object ID
                    </label>
                    <Input
                      id="viewer-object-id"
                      value={viewerObjectId}
                      onChange={(event) => setViewerObjectId(event.target.value)}
                      placeholder=""
                      className="h-12 rounded-lg border-slate-200 text-base focus:border-[#2563eb] focus:ring-[3px] focus:ring-blue-100"
                    />
                  </div>

                  <Button
                    type="submit"
                    className="h-12 rounded-lg bg-slate-600 px-5 text-base text-white shadow-none hover:translate-y-0 hover:bg-slate-700"
                  >
                    Search
                  </Button>
                </div>
              </form>
            </div>

            {viewerSearchMessage ? (
              <div className="max-w-4xl rounded-xl border border-slate-200 bg-slate-50 px-5 py-4 text-sm leading-7 text-slate-600">
                {viewerSearchMessage}
              </div>
            ) : null}
          </section>
        ) : null}

        {activeView === "data-model" ? (
          <section className="space-y-4">
            <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-end">
              <Button
                variant="outline"
                type="button"
                className="h-10 rounded-lg border-slate-200 px-4 text-sm shadow-none hover:translate-y-0"
              >
                <Download className="size-4" />
                Export data
              </Button>
              <Button
                variant="default"
                onClick={openCreateModal}
                type="button"
                className="h-10 rounded-lg bg-[#2563eb] px-4 text-sm text-white shadow-none hover:translate-y-0 hover:bg-[#1d4ed8]"
              >
                <Plus className="size-4" />
                Create a new table
              </Button>
            </div>

            <div className="space-y-4">
              {sortedTables.length > 0 ? (
                sortedTables.map((table) => {
                  const assembledTable = assembledTables.find((item) => item.id === table.id);
                  const fields = assembledTable
                    ? Object.values(assembledTable.fields).sort((a, b) =>
                        a.name.localeCompare(b.name)
                      )
                    : [];
                  const links = assembledTable
                    ? Object.values(assembledTable.links_to_single)
                    : [];
                  const isExpanded = expandedTableIds.includes(table.id);
                  const isMenuOpen = openTableMenuId === table.id;

                  return (
                    <div
                      key={table.id}
                      className={cn(
                        "rounded-xl border bg-white",
                        isExpanded ? "border-[#93c5fd]" : "border-slate-200"
                      )}
                    >
                      <div className="flex flex-col gap-4 px-4 py-4 lg:flex-row lg:items-center lg:justify-between">
                        <div className="min-w-0">
                          <p className="text-[1.05rem] font-semibold tracking-tight text-slate-950">
                            {table.name}
                          </p>
                        </div>
                        <div className="flex flex-wrap items-center gap-2">
                          <div className="relative">
                            <button
                              type="button"
                              onClick={() => toggleTableMenu(table.id)}
                              className="inline-flex size-9 items-center justify-center rounded-lg border border-[#c7d2fe] bg-white text-[#2563eb] transition-colors hover:bg-[#f8fbff]"
                              aria-label={`Open ${table.name} actions`}
                              aria-expanded={isMenuOpen}
                            >
                              <MoreHorizontal className="size-4" />
                            </button>
                            {isMenuOpen ? (
                              <div className="absolute right-0 top-11 z-20 w-52 overflow-hidden rounded-xl border border-slate-200 bg-white shadow-[0_16px_38px_rgba(15,23,42,0.12)]">
                                <button
                                  type="button"
                                  onClick={() => {
                                    setOpenTableMenuId(null);
                                    openCreateFieldModal(table);
                                  }}
                                  className="flex w-full items-center gap-3 px-4 py-3 text-left text-sm text-slate-900 transition-colors hover:bg-slate-50"
                                >
                                  <Plus className="size-4 text-[#2563eb]" />
                                  Create a new field
                                </button>
                                <button
                                  type="button"
                                  onClick={() => {
                                    setOpenTableMenuId(null);
                                    openCreateLinkModal(table);
                                  }}
                                  className="flex w-full items-center gap-3 px-4 py-3 text-left text-sm text-slate-900 transition-colors hover:bg-slate-50"
                                >
                                  <Plus className="size-4 text-[#2563eb]" />
                                  Create a link
                                </button>
                                <button
                                  type="button"
                                  disabled
                                  className="flex w-full items-center justify-between gap-3 px-4 py-3 text-left text-sm text-slate-900"
                                >
                                  <span className="flex items-center gap-3">
                                    <Plus className="size-4 text-[#2563eb]" />
                                    Create a pivot
                                  </span>
                                  <span className="rounded-full bg-slate-100 px-2 py-0.5 text-[11px] font-medium uppercase tracking-[0.08em] text-slate-500">
                                    Soon
                                  </span>
                                </button>
                              </div>
                            ) : null}
                          </div>
                          <Link
                            href={`/your-data/upload/${encodeURIComponent(table.name)}`}
                            className="inline-flex h-9 items-center justify-center gap-2 rounded-lg border border-[#c7d2fe] bg-white px-3 text-sm font-medium text-[#2563eb] transition-colors hover:bg-[#f8fbff]"
                          >
                            <Upload className="size-4" />
                            Upload data
                          </Link>
                          <button
                            type="button"
                            className="inline-flex size-9 items-center justify-center rounded-lg border border-slate-200 bg-white text-[#2563eb] transition-colors hover:bg-slate-50"
                            onClick={() => openEditModal(table)}
                            aria-label={`Edit ${table.name}`}
                          >
                            <Pencil className="size-4" />
                          </button>
                          <button
                            type="button"
                            className="inline-flex size-9 items-center justify-center rounded-lg border border-slate-200 bg-white text-[#2563eb] transition-colors hover:bg-slate-50"
                            onClick={() => openDeleteModal(table)}
                            aria-label={`Delete ${table.name}`}
                          >
                            <Trash2 className="size-4" />
                          </button>
                          <button
                            type="button"
                            onClick={() => toggleExpandedTable(table.id)}
                            className="inline-flex size-9 items-center justify-center rounded-lg border border-slate-200 bg-white text-slate-700 transition-colors hover:bg-slate-50"
                            aria-label={isExpanded ? `Collapse ${table.name}` : `Expand ${table.name}`}
                          >
                            {isExpanded ? (
                              <ChevronUp className="size-4" />
                            ) : (
                              <ChevronDown className="size-4" />
                            )}
                          </button>
                        </div>
                      </div>

                      {isExpanded ? (
                        <div className="border-t border-slate-200 px-4 py-4">
                          {table.description ? (
                            <div className="mb-4 inline-flex items-center gap-3 rounded-lg bg-slate-100 px-4 py-3 text-sm text-slate-800">
                              <span>{table.description}</span>
                              <button
                                type="button"
                                onClick={() => openEditModal(table)}
                                className="inline-flex size-8 items-center justify-center rounded-lg bg-white text-slate-700 transition-colors hover:bg-slate-50"
                                aria-label={`Edit description for ${table.name}`}
                              >
                                <Pencil className="size-4" />
                              </button>
                            </div>
                          ) : null}

                          <div className="overflow-hidden rounded-xl border border-slate-200">
                            <table className="w-full border-collapse text-left">
                              <thead className="bg-white">
                                <tr className="border-b border-slate-200 text-sm text-slate-900">
                                  <th className="px-4 py-3 font-semibold">Name</th>
                                  <th className="px-4 py-3 font-semibold">Type</th>
                                  <th className="px-4 py-3 font-semibold">Required</th>
                                  <th className="px-4 py-3 font-semibold">Unique</th>
                                  <th className="px-4 py-3 font-semibold">Description</th>
                                  <th className="px-4 py-3 font-semibold" />
                                </tr>
                              </thead>
                              <tbody>
                                {fields.length > 0 ? (
                                  fields.map((field, index) => (
                                    <tr
                                      key={field.id}
                                      className={cn(
                                        "border-b border-slate-100 text-sm text-slate-900 last:border-b-0",
                                        index % 2 === 1 && "bg-slate-50/60"
                                      )}
                                    >
                                      <td className="px-4 py-3">{field.name}</td>
                                      <td className="px-4 py-3">{formatFieldType(field.data_type)}</td>
                                      <td className="px-4 py-3">
                                        {field.nullable ? "Optional" : "Required"}
                                      </td>
                                      <td className="px-4 py-3">
                                        {field.is_unique ? "Unique" : ""}
                                      </td>
                                      <td className="px-4 py-3">
                                        {field.description || "No description"}
                                      </td>
                                      <td className="px-4 py-3">
                                        <div className="flex justify-end gap-2">
                                          <button
                                            type="button"
                                            onClick={() => openEditFieldModal(table, field)}
                                            className="inline-flex size-9 items-center justify-center rounded-lg border border-slate-200 bg-white text-[#2563eb] transition-colors hover:bg-slate-50"
                                            aria-label={`Edit field ${field.name}`}
                                          >
                                            <Pencil className="size-4" />
                                          </button>
                                          <button
                                            type="button"
                                            onClick={() => openDeleteFieldModal(table, field)}
                                            className="inline-flex size-9 items-center justify-center rounded-lg border border-slate-200 bg-white text-[#2563eb] transition-colors hover:bg-slate-50"
                                            aria-label={`Delete field ${field.name}`}
                                          >
                                            <Trash2 className="size-4" />
                                          </button>
                                        </div>
                                      </td>
                                    </tr>
                                  ))
                                ) : (
                                  <tr>
                                    <td
                                      className="px-4 py-4 text-sm text-slate-500"
                                      colSpan={6}
                                    >
                                      No fields yet for this table.
                                    </td>
                                  </tr>
                                )}
                              </tbody>
                            </table>
                          </div>

                          <div className="mt-6">
                            <p className="mb-3 text-sm text-slate-700">
                              Links from other entities from{" "}
                              <span className="font-semibold">{table.name}</span>
                            </p>
                            <div className="overflow-hidden rounded-xl border border-slate-200">
                              <table className="w-full border-collapse text-left">
                                <thead className="bg-white">
                                  <tr className="border-b border-slate-200 text-sm text-slate-900">
                                    <th className="px-4 py-3 font-semibold">Foreign Key</th>
                                    <th className="px-4 py-3 font-semibold">Related table</th>
                                    <th className="px-4 py-3 font-semibold">Parent table field name</th>
                                    <th className="px-4 py-3 font-semibold">Usage</th>
                                    <th className="px-4 py-3 font-semibold" />
                                  </tr>
                                </thead>
                                <tbody>
                                  {links.length > 0 ? (
                                    links.map((link) => {
                                      const childTableName = getTableNameById(link.child_table_id);
                                      const childFieldName = getFieldNameById(
                                        link.child_table_id,
                                        link.child_field_id
                                      );
                                      const parentTableName = getTableNameById(link.parent_table_id);
                                      const parentFieldName = getFieldNameById(
                                        link.parent_table_id,
                                        link.parent_field_id
                                      );

                                      return (
                                        <tr
                                          key={link.id}
                                          className="border-b border-slate-100 text-sm text-slate-900 last:border-b-0"
                                        >
                                          <td className="px-4 py-3">{childFieldName}</td>
                                          <td className="px-4 py-3">{parentTableName}</td>
                                          <td className="px-4 py-3">{parentFieldName}</td>
                                          <td className="px-4 py-3 text-slate-600">
                                            {`${childTableName}.${childFieldName} -> ${parentTableName}.${parentFieldName}`}
                                          </td>
                                          <td className="px-4 py-3">
                                            <div className="flex justify-end gap-2">
                                              <button
                                                type="button"
                                                className="inline-flex size-9 items-center justify-center rounded-lg border border-slate-200 bg-white text-[#2563eb] transition-colors hover:bg-slate-50"
                                                aria-label={`Delete link ${link.name}`}
                                              >
                                                <Trash2 className="size-4" />
                                              </button>
                                            </div>
                                          </td>
                                        </tr>
                                      );
                                    })
                                  ) : (
                                    <tr>
                                      <td
                                        className="px-4 py-4 text-sm text-slate-500"
                                        colSpan={5}
                                      >
                                        No links configured for this table yet.
                                      </td>
                                    </tr>
                                  )}
                                </tbody>
                              </table>
                            </div>
                          </div>
                        </div>
                      ) : null}
                    </div>
                  );
                })
              ) : (
                <div className="rounded-xl border border-dashed border-slate-300 bg-slate-50 p-5 text-sm text-slate-600">
                  No tables yet. Create your first table to start modeling the tenant schema.
                </div>
              )}
            </div>
          </section>
        ) : null}
      </div>

      {typeof document !== "undefined" && isCreateModalOpen
        ? createPortal(
        <div className="fixed inset-0 z-[100] flex items-center justify-center bg-slate-950/38 p-4">
          <div className="w-full max-w-[560px] overflow-hidden rounded-xl border border-slate-200 bg-white shadow-[0_24px_70px_rgba(15,23,42,0.14)]">
            <div className="relative border-b border-slate-200 px-6 py-5 text-center">
              <h3 className="text-[1.65rem] font-semibold tracking-tight text-slate-950">
                {editingTable ? "Edit table" : "Create a new table"}
              </h3>
              <button
                type="button"
                onClick={closeCreateModal}
                className="absolute top-4 right-4 rounded-lg p-2 text-slate-400 hover:bg-slate-100 hover:text-slate-900"
                aria-label="Close create table modal"
              >
                <X className="size-4" />
              </button>
            </div>

            <form className="px-6 py-6" onSubmit={handleCreateTableSubmit}>
              <div className="space-y-5">
                <div className="space-y-2">
                  <label className="text-[15px] font-medium text-slate-900" htmlFor="table-name">
                    Name
                  </label>
                  <Input
                    id="table-name"
                    value={name}
                    onChange={(event) => setName(event.target.value)}
                    placeholder="Table name"
                    autoFocus
                    disabled={Boolean(editingTable)}
                    className="h-11 rounded-md border-slate-200 focus:border-[#2563eb] focus:ring-[3px] focus:ring-blue-100"
                  />
                </div>

                <div className="space-y-2">
                  <label className="text-[15px] font-medium text-slate-900" htmlFor="table-alias">
                    Alias
                  </label>
                  <Input
                    id="table-alias"
                    value={alias}
                    onChange={(event) => setAlias(event.target.value)}
                    placeholder="Display alias"
                    className="h-11 rounded-md border-slate-200 focus:border-[#2563eb] focus:ring-[3px] focus:ring-blue-100"
                  />
                </div>

                <div className="grid gap-5 md:grid-cols-[1fr_180px]">
                  <div className="space-y-2">
                    <label
                      className="text-[15px] font-medium text-slate-900"
                      htmlFor="table-description"
                    >
                      Description
                    </label>
                    <Input
                      id="table-description"
                      value={description}
                      onChange={(event) => setDescription(event.target.value)}
                      placeholder="Table description"
                      className="h-11 rounded-md border-slate-200 focus:border-[#2563eb] focus:ring-[3px] focus:ring-blue-100"
                    />
                  </div>

                  <div className="space-y-2">
                    <label
                      className="text-[15px] font-medium text-slate-900"
                      htmlFor="semantic-type"
                    >
                      Semantic type
                    </label>
                    <select
                      id="semantic-type"
                      value={semanticType}
                      onChange={(event) =>
                        setSemanticType(event.target.value as (typeof semanticTypes)[number])
                      }
                      className="flex h-11 w-full rounded-md border border-slate-200 bg-white px-3 text-sm text-slate-900 outline-none focus:border-[#2563eb] focus:ring-[3px] focus:ring-blue-100"
                    >
                      {semanticTypes.map((type) => (
                        <option key={type} value={type}>
                          {type}
                        </option>
                      ))}
                    </select>
                  </div>
                </div>
              </div>

              {formError ? (
                <div className="mt-5 rounded-md border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">
                  {formError}
                </div>
              ) : null}

              <div className="mt-6 flex items-center justify-end gap-3 border-t border-slate-200 pt-4">
                <Button
                  variant="outline"
                  type="button"
                  onClick={closeCreateModal}
                  className="h-9 rounded-lg border-slate-200 px-4 text-sm shadow-none hover:translate-y-0"
                >
                  Cancel
                </Button>
                <Button
                  variant="default"
                  type="submit"
                  disabled={createTableMutation.isPending || updateTableMutation.isPending}
                  className="h-9 rounded-lg bg-[#2563eb] px-4 text-sm text-white shadow-none hover:translate-y-0 hover:bg-[#1d4ed8]"
                >
                  {createTableMutation.isPending || updateTableMutation.isPending
                    ? editingTable
                      ? "Saving..."
                      : "Creating..."
                    : editingTable
                      ? "Save changes"
                      : "Create table"}
                </Button>
              </div>
            </form>
          </div>
        </div>,
        document.body
      )
        : null}

      {typeof document !== "undefined" && isCreateFieldModalOpen && fieldTable
        ? createPortal(
        <div className="fixed inset-0 z-[105] flex items-center justify-center bg-slate-950/38 p-4">
          <div className="flex max-h-[calc(100vh-2rem)] w-full max-w-[560px] flex-col overflow-hidden rounded-xl border border-slate-200 bg-white shadow-[0_24px_70px_rgba(15,23,42,0.14)]">
            <div className="relative border-b border-slate-200 px-6 py-5 text-center">
              <h3 className="text-[1.65rem] font-semibold tracking-tight text-slate-950">
                Create a new field
              </h3>
              <button
                type="button"
                onClick={closeCreateFieldModal}
                className="absolute top-4 right-4 rounded-lg p-2 text-slate-400 hover:bg-slate-100 hover:text-slate-900"
                aria-label="Close create field modal"
              >
                <X className="size-4" />
              </button>
            </div>

            <form className="flex min-h-0 flex-1 flex-col" onSubmit={handleCreateFieldSubmit}>
              <div className="min-h-0 flex-1 space-y-5 overflow-y-auto px-6 py-6">
                <div className="space-y-2">
                  <label className="text-[15px] font-medium text-slate-900" htmlFor="field-name">
                    Name
                  </label>
                  <Input
                    id="field-name"
                    value={fieldName}
                    onChange={(event) => setFieldName(event.target.value)}
                    placeholder="Field name"
                    autoFocus
                    className="h-11 rounded-md border-slate-200 focus:border-[#2563eb] focus:ring-[3px] focus:ring-blue-100"
                  />
                </div>

                <div className="space-y-2">
                  <label
                    className="text-[15px] font-medium text-slate-900"
                    htmlFor="field-description"
                  >
                    Description
                  </label>
                  <Input
                    id="field-description"
                    value={fieldDescription}
                    onChange={(event) => setFieldDescription(event.target.value)}
                    placeholder="Field description"
                    className="h-11 rounded-md border-slate-200 focus:border-[#2563eb] focus:ring-[3px] focus:ring-blue-100"
                  />
                </div>

                <div className="grid gap-5 md:grid-cols-2">
                  <div className="space-y-2">
                    <label className="text-[15px] font-medium text-slate-900" htmlFor="field-required">
                      Required
                    </label>
                    <select
                      id="field-required"
                      value={fieldIsRequired ? "required" : "optional"}
                      onChange={(event) => setFieldIsRequired(event.target.value === "required")}
                      className="flex h-11 w-full rounded-md border border-slate-200 bg-white px-3 text-sm text-slate-900 outline-none focus:border-[#2563eb] focus:ring-[3px] focus:ring-blue-100"
                    >
                      <option value="optional">Optional</option>
                      <option value="required">Required</option>
                    </select>
                  </div>

                  <div className="space-y-2">
                    <label className="text-[15px] font-medium text-slate-900" htmlFor="field-type">
                      Type
                    </label>
                    <select
                      id="field-type"
                      value={fieldType}
                      onChange={(event) =>
                        setFieldType(
                          event.target.value as
                            | "bool"
                            | "int"
                            | "float"
                            | "string"
                            | "timestamp"
                            | "ip_address"
                        )
                      }
                      className="flex h-11 w-full rounded-md border border-slate-200 bg-white px-3 text-sm text-slate-900 outline-none focus:border-[#2563eb] focus:ring-[3px] focus:ring-blue-100"
                    >
                      <option value="string">String</option>
                      <option value="int">Integer</option>
                      <option value="float">Float</option>
                      <option value="bool">Boolean</option>
                      <option value="timestamp">Timestamp</option>
                      <option value="ip_address">IP address</option>
                    </select>
                  </div>
                </div>

                <div className="space-y-4">
                  <label className="flex items-start gap-3">
                    <input
                      type="checkbox"
                      checked={fieldIsEnum}
                      onChange={(event) => {
                        const checked = event.target.checked;
                        setFieldIsEnum(checked);
                        if (checked && fieldEnumValues.length === 0) {
                          setFieldEnumValues([{ value: "", label: "" }]);
                        }
                      }}
                      className="mt-1 size-5 rounded border border-slate-300 text-[#2563eb] focus:ring-[#2563eb]"
                    />
                    <span>
                      <span className="block text-[15px] font-medium text-slate-900">
                        This field is an enumerated value
                      </span>
                      <span className="block text-sm leading-6 text-slate-500">
                        It takes a limited number of distinct values, like a status code
                      </span>
                    </span>
                  </label>

                  {fieldIsEnum ? (
                    <div className="space-y-3 rounded-lg border border-slate-200 bg-slate-50/70 p-4">
                      <div className="flex items-center justify-between gap-3">
                        <div>
                          <p className="text-sm font-medium text-slate-900">Enum values</p>
                          <p className="text-sm text-slate-500">
                            Provide the allowed value set for this field.
                          </p>
                        </div>
                        <Button
                          type="button"
                          variant="outline"
                          onClick={addEnumValueRow}
                          className="h-8 rounded-lg border-slate-200 px-3 text-sm shadow-none hover:translate-y-0"
                        >
                          <Plus className="size-4" />
                          Add value
                        </Button>
                      </div>

                      <div className="max-h-64 space-y-3 overflow-y-auto pr-1">
                        {fieldEnumValues.map((item, index) => (
                          <div key={item.id ?? `new-${index}`} className="grid gap-3 md:grid-cols-[1fr_1fr_auto]">
                            <Input
                              value={item.value}
                              onChange={(event) =>
                                updateEnumValueRow(index, "value", event.target.value)
                              }
                              placeholder="Value"
                              className="h-10 rounded-md border-slate-200 focus:border-[#2563eb] focus:ring-[3px] focus:ring-blue-100"
                            />
                            <Input
                              value={item.label}
                              onChange={(event) =>
                                updateEnumValueRow(index, "label", event.target.value)
                              }
                              placeholder="Label"
                              className="h-10 rounded-md border-slate-200 focus:border-[#2563eb] focus:ring-[3px] focus:ring-blue-100"
                            />
                            <button
                              type="button"
                              onClick={() => removeEnumValueRow(index)}
                              className="inline-flex size-10 items-center justify-center rounded-lg border border-slate-200 bg-white text-slate-500 transition-colors hover:bg-slate-50 hover:text-slate-900"
                              aria-label={`Remove enum value row ${index + 1}`}
                            >
                              <Trash2 className="size-4" />
                            </button>
                          </div>
                        ))}
                      </div>
                    </div>
                  ) : null}

                  <label className="flex items-start gap-3">
                    <input
                      type="checkbox"
                      checked={fieldIsUnique}
                      onChange={(event) => setFieldIsUnique(event.target.checked)}
                      className="mt-1 size-5 rounded border border-slate-300 text-[#2563eb] focus:ring-[#2563eb]"
                    />
                    <span>
                      <span className="block text-[15px] font-medium text-slate-900">
                        This field is unique
                      </span>
                      <span className="block text-sm leading-6 text-slate-500">
                        Duplicate values are not allowed for this field
                      </span>
                    </span>
                  </label>
                </div>
              </div>

              {fieldFormError ? (
                <div className="mx-6 mt-5 rounded-md border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">
                  {fieldFormError}
                </div>
              ) : null}

              <div className="mt-6 flex items-center justify-end gap-3 border-t border-slate-200 px-6 py-4">
                <Button
                  variant="outline"
                  type="button"
                  onClick={closeCreateFieldModal}
                  className="h-9 rounded-lg border-slate-200 px-4 text-sm shadow-none hover:translate-y-0"
                >
                  Cancel
                </Button>
                <Button
                  variant="default"
                  type="submit"
                  disabled={createFieldMutation.isPending}
                  className="h-9 rounded-lg bg-[#2563eb] px-4 text-sm text-white shadow-none hover:translate-y-0 hover:bg-[#1d4ed8]"
                >
                  {createFieldMutation.isPending ? "Creating..." : "Create"}
                </Button>
              </div>
            </form>
          </div>
        </div>,
        document.body
      )
        : null}

      {typeof document !== "undefined" && isCreateLinkModalOpen
        ? createPortal(
        <div className="fixed inset-0 z-[105] flex items-center justify-center bg-slate-950/38 p-4">
          <div className="flex max-h-[calc(100vh-2rem)] w-full max-w-[560px] flex-col overflow-hidden rounded-xl border border-slate-200 bg-white shadow-[0_24px_70px_rgba(15,23,42,0.14)]">
            <div className="relative border-b border-slate-200 px-6 py-5 text-center">
              <h3 className="text-[1.65rem] font-semibold tracking-tight text-slate-950">
                Create a new link
              </h3>
              <button
                type="button"
                onClick={closeCreateLinkModal}
                className="absolute top-4 right-4 rounded-lg p-2 text-slate-400 hover:bg-slate-100 hover:text-slate-900"
                aria-label="Close create link modal"
              >
                <X className="size-4" />
              </button>
            </div>

            <form className="flex min-h-0 flex-1 flex-col" onSubmit={handleCreateLinkSubmit}>
              <div className="min-h-0 flex-1 space-y-5 overflow-y-auto px-6 py-6">
                <div className="space-y-2">
                  <label className="text-[15px] font-medium text-slate-900" htmlFor="link-name">
                    Name
                  </label>
                  <Input
                    id="link-name"
                    value={linkName}
                    onChange={(event) => setLinkName(event.target.value)}
                    placeholder="account"
                    autoFocus
                    className="h-11 rounded-md border-slate-200 focus:border-[#2563eb] focus:ring-[3px] focus:ring-blue-100"
                  />
                </div>

                <div className="grid gap-5 md:grid-cols-2">
                  <div className="space-y-2">
                    <label className="text-[15px] font-medium text-slate-900" htmlFor="link-child-table">
                      On the table
                    </label>
                    <select
                      id="link-child-table"
                      value={linkChildTableId}
                      disabled
                      className="flex h-11 w-full rounded-md border border-slate-200 bg-slate-50 px-3 text-sm text-slate-500 outline-none"
                    >
                      <option value={linkChildTableId}>{childTable?.name ?? "Select table"}</option>
                    </select>
                  </div>

                  <div className="space-y-2">
                    <label className="text-[15px] font-medium text-slate-900" htmlFor="link-child-field">
                      the field
                    </label>
                    <select
                      id="link-child-field"
                      value={linkChildFieldId}
                      onChange={(event) => setLinkChildFieldId(event.target.value)}
                      className="flex h-11 w-full rounded-md border border-slate-200 bg-white px-3 text-sm text-slate-900 outline-none focus:border-[#2563eb] focus:ring-[3px] focus:ring-blue-100"
                    >
                      {childFieldOptions.map((field) => (
                        <option key={field.id} value={field.id}>
                          {field.name}
                        </option>
                      ))}
                    </select>
                  </div>
                </div>

                <div className="grid gap-5 md:grid-cols-2">
                  <div className="space-y-2">
                    <label className="text-[15px] font-medium text-slate-900" htmlFor="link-parent-table">
                      refers to the table
                    </label>
                    <select
                      id="link-parent-table"
                      value={linkParentTableId}
                      onChange={(event) => handleLinkParentTableChange(event.target.value)}
                      className="flex h-11 w-full rounded-md border border-slate-200 bg-white px-3 text-sm text-slate-900 outline-none focus:border-[#2563eb] focus:ring-[3px] focus:ring-blue-100"
                    >
                      <option value="">Select table</option>
                      {assembledTables
                        .filter((tableOption) => tableOption.id !== linkChildTableId)
                        .sort((a, b) => a.name.localeCompare(b.name))
                        .map((tableOption) => (
                          <option key={tableOption.id} value={tableOption.id}>
                            {tableOption.name}
                          </option>
                        ))}
                    </select>
                  </div>

                  <div className="space-y-2">
                    <label className="text-[15px] font-medium text-slate-900" htmlFor="link-parent-field">
                      on the field
                    </label>
                    <select
                      id="link-parent-field"
                      value={linkParentFieldId}
                      onChange={(event) => setLinkParentFieldId(event.target.value)}
                      className="flex h-11 w-full rounded-md border border-slate-200 bg-white px-3 text-sm text-slate-900 outline-none focus:border-[#2563eb] focus:ring-[3px] focus:ring-blue-100"
                    >
                      <option value="">Select field</option>
                      {parentFieldOptions.map((field) => (
                        <option
                          key={field.id}
                          value={field.id}
                          disabled={!field.is_unique}
                        >
                          {field.is_unique ? field.name : `${field.name} (mark as unique first)`}
                        </option>
                      ))}
                    </select>
                  </div>
                </div>

                <p className="text-sm leading-7 text-slate-600">
                  A link must point to a unique field in the target table. If you want to point to another field,
                  mark it as unique first.
                </p>
              </div>

              {linkFormError ? (
                <div className="mx-6 mt-5 rounded-md border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">
                  {linkFormError}
                </div>
              ) : null}

              <div className="mt-6 flex items-center justify-end gap-3 border-t border-slate-200 px-6 py-4">
                <Button
                  variant="outline"
                  type="button"
                  onClick={closeCreateLinkModal}
                  className="h-9 rounded-lg border-slate-200 px-4 text-sm shadow-none hover:translate-y-0"
                >
                  Cancel
                </Button>
                <Button
                  variant="default"
                  type="submit"
                  disabled={createLinkMutation.isPending}
                  className="h-9 rounded-lg bg-[#2563eb] px-4 text-sm text-white shadow-none hover:translate-y-0 hover:bg-[#1d4ed8]"
                >
                  {createLinkMutation.isPending ? "Creating..." : "Create link"}
                </Button>
              </div>
            </form>
          </div>
        </div>,
        document.body
      )
        : null}

      {typeof document !== "undefined" && isEditFieldModalOpen && fieldTable && editingField
        ? createPortal(
        <div className="fixed inset-0 z-[106] flex items-center justify-center bg-slate-950/38 p-4">
          <div className="flex max-h-[calc(100vh-2rem)] w-full max-w-[560px] flex-col overflow-hidden rounded-xl border border-slate-200 bg-white shadow-[0_24px_70px_rgba(15,23,42,0.14)]">
            <div className="relative border-b border-slate-200 px-6 py-5 text-center">
              <h3 className="text-[1.65rem] font-semibold tracking-tight text-slate-950">
                Edit field
              </h3>
              <button
                type="button"
                onClick={closeEditFieldModal}
                className="absolute top-4 right-4 rounded-lg p-2 text-slate-400 hover:bg-slate-100 hover:text-slate-900"
                aria-label="Close edit field modal"
              >
                <X className="size-4" />
              </button>
            </div>

            <form className="flex min-h-0 flex-1 flex-col" onSubmit={handleEditFieldSubmit}>
              <div className="min-h-0 flex-1 space-y-5 overflow-y-auto px-6 py-6">
                <div className="space-y-2">
                  <label className="text-[15px] font-medium text-slate-900" htmlFor="edit-field-name">
                    Name
                  </label>
                  <Input
                    id="edit-field-name"
                    value={fieldName}
                    disabled
                    className="h-11 rounded-md border-slate-200 bg-slate-50 text-slate-500"
                  />
                </div>

                <div className="space-y-2">
                  <label className="text-[15px] font-medium text-slate-900" htmlFor="edit-field-description">
                    Description
                  </label>
                  <Input
                    id="edit-field-description"
                    value={fieldDescription}
                    onChange={(event) => setFieldDescription(event.target.value)}
                    placeholder="Field description"
                    autoFocus
                    className="h-11 rounded-md border-slate-200 focus:border-[#2563eb] focus:ring-[3px] focus:ring-blue-100"
                  />
                </div>

                <div className="grid gap-5 md:grid-cols-2">
                  <div className="space-y-2">
                    <label className="text-[15px] font-medium text-slate-900" htmlFor="edit-field-required">
                      Required
                    </label>
                    <select
                      id="edit-field-required"
                      value={fieldIsRequired ? "required" : "optional"}
                      onChange={(event) => setFieldIsRequired(event.target.value === "required")}
                      className="flex h-11 w-full rounded-md border border-slate-200 bg-white px-3 text-sm text-slate-900 outline-none focus:border-[#2563eb] focus:ring-[3px] focus:ring-blue-100"
                    >
                      <option value="optional">Optional</option>
                      <option value="required">Required</option>
                    </select>
                  </div>

                  <div className="space-y-2">
                    <label className="text-[15px] font-medium text-slate-900" htmlFor="edit-field-type">
                      Type
                    </label>
                    <Input
                      id="edit-field-type"
                      value={formatFieldType(fieldType)}
                      disabled
                      className="h-11 rounded-md border-slate-200 bg-slate-50 text-slate-500"
                    />
                  </div>
                </div>

                <div className="space-y-4">
                  <label className="flex items-start gap-3">
                    <input
                      type="checkbox"
                      checked={fieldIsEnum}
                      onChange={(event) => {
                        const checked = event.target.checked;
                        setFieldIsEnum(checked);
                        if (checked && fieldEnumValues.length === 0) {
                          setFieldEnumValues([{ value: "", label: "" }]);
                        }
                      }}
                      className="mt-1 size-5 rounded border border-slate-300 text-[#2563eb] focus:ring-[#2563eb]"
                    />
                    <span>
                      <span className="block text-[15px] font-medium text-slate-900">
                        This field is an enumerated value
                      </span>
                      <span className="block text-sm leading-6 text-slate-500">
                        It takes a limited number of distinct values, like a status code
                      </span>
                    </span>
                  </label>

                  {fieldIsEnum ? (
                    <div className="space-y-3 rounded-lg border border-slate-200 bg-slate-50/70 p-4">
                      <div className="flex items-center justify-between gap-3">
                        <div>
                          <p className="text-sm font-medium text-slate-900">Enum values</p>
                          <p className="text-sm text-slate-500">
                            Keep the allowed value set in sync for this field.
                          </p>
                        </div>
                        <Button
                          type="button"
                          variant="outline"
                          onClick={addEnumValueRow}
                          className="h-8 rounded-lg border-slate-200 px-3 text-sm shadow-none hover:translate-y-0"
                        >
                          <Plus className="size-4" />
                          Add value
                        </Button>
                      </div>

                      <div className="max-h-64 space-y-3 overflow-y-auto pr-1">
                        {fieldEnumValues.map((item, index) => (
                          <div key={item.id ?? `edit-new-${index}`} className="grid gap-3 md:grid-cols-[1fr_1fr_auto]">
                            <Input
                              value={item.value}
                              onChange={(event) =>
                                updateEnumValueRow(index, "value", event.target.value)
                              }
                              placeholder="Value"
                              className="h-10 rounded-md border-slate-200 focus:border-[#2563eb] focus:ring-[3px] focus:ring-blue-100"
                            />
                            <Input
                              value={item.label}
                              onChange={(event) =>
                                updateEnumValueRow(index, "label", event.target.value)
                              }
                              placeholder="Label"
                              className="h-10 rounded-md border-slate-200 focus:border-[#2563eb] focus:ring-[3px] focus:ring-blue-100"
                            />
                            <button
                              type="button"
                              onClick={() => removeEnumValueRow(index)}
                              className="inline-flex size-10 items-center justify-center rounded-lg border border-slate-200 bg-white text-slate-500 transition-colors hover:bg-slate-50 hover:text-slate-900"
                              aria-label={`Remove enum value row ${index + 1}`}
                            >
                              <Trash2 className="size-4" />
                            </button>
                          </div>
                        ))}
                      </div>
                    </div>
                  ) : null}

                  <label className="flex items-start gap-3">
                    <input
                      type="checkbox"
                      checked={fieldIsUnique}
                      onChange={(event) => setFieldIsUnique(event.target.checked)}
                      className="mt-1 size-5 rounded border border-slate-300 text-[#2563eb] focus:ring-[#2563eb]"
                    />
                    <span>
                      <span className="block text-[15px] font-medium text-slate-900">
                        This field is unique
                      </span>
                      <span className="block text-sm leading-6 text-slate-500">
                        Duplicate values are not allowed for this field
                      </span>
                    </span>
                  </label>
                </div>
              </div>

              {fieldFormError ? (
                <div className="mx-6 mt-5 rounded-md border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">
                  {fieldFormError}
                </div>
              ) : null}

              <div className="mt-6 flex items-center justify-end gap-3 border-t border-slate-200 px-6 py-4">
                <Button
                  variant="outline"
                  type="button"
                  onClick={closeEditFieldModal}
                  className="h-9 rounded-lg border-slate-200 px-4 text-sm shadow-none hover:translate-y-0"
                >
                  Cancel
                </Button>
                <Button
                  variant="default"
                  type="submit"
                  disabled={
                    updateFieldMutation.isPending ||
                    createFieldEnumValueMutation.isPending ||
                    updateFieldEnumValueMutation.isPending ||
                    deleteFieldEnumValueMutation.isPending
                  }
                  className="h-9 rounded-lg bg-[#2563eb] px-4 text-sm text-white shadow-none hover:translate-y-0 hover:bg-[#1d4ed8]"
                >
                  {updateFieldMutation.isPending ||
                  createFieldEnumValueMutation.isPending ||
                  updateFieldEnumValueMutation.isPending ||
                  deleteFieldEnumValueMutation.isPending
                    ? "Saving..."
                    : "Save changes"}
                </Button>
              </div>
            </form>
          </div>
        </div>,
        document.body
      )
        : null}

      {typeof document !== "undefined" && isDeleteModalOpen && pendingDeleteTable
        ? createPortal(
        <div className="fixed inset-0 z-[110] flex items-center justify-center bg-slate-950/42 p-4">
          <div className="w-full max-w-[460px] overflow-hidden rounded-xl border border-slate-200 bg-white shadow-[0_24px_70px_rgba(15,23,42,0.16)]">
            <div className="border-b border-slate-200 px-6 py-5">
              <h3 className="text-xl font-semibold tracking-tight text-slate-950">
                Delete table?
              </h3>
              <p className="mt-2 text-sm leading-7 text-slate-600">
                This will permanently delete <span className="font-semibold text-slate-950">{pendingDeleteTable.name}</span>.
                If the table has dependencies, the backend may block the delete.
              </p>
            </div>
            {formError ? (
              <div className="mx-6 mt-5 rounded-md border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">
                {formError}
              </div>
            ) : null}
            <div className="flex items-center justify-end gap-3 px-6 py-5">
              <Button
                variant="outline"
                type="button"
                onClick={closeDeleteModal}
                className="h-9 rounded-lg border-slate-200 px-4 text-sm shadow-none hover:translate-y-0"
              >
                Cancel
              </Button>
              <Button
                type="button"
                onClick={handleDeleteTable}
                disabled={deleteTableMutation.isPending}
                className="h-9 rounded-lg bg-red-600 px-4 text-sm text-white shadow-none hover:translate-y-0 hover:bg-red-700"
              >
                {deleteTableMutation.isPending ? "Deleting..." : "Delete table"}
              </Button>
            </div>
          </div>
        </div>,
        document.body
      )
        : null}

      {typeof document !== "undefined" && isDeleteFieldModalOpen && pendingDeleteField
        ? createPortal(
        <div className="fixed inset-0 z-[111] flex items-center justify-center bg-slate-950/42 p-4">
          <div className="w-full max-w-[460px] overflow-hidden rounded-xl border border-slate-200 bg-white shadow-[0_24px_70px_rgba(15,23,42,0.16)]">
            <div className="border-b border-slate-200 px-6 py-5">
              <h3 className="text-xl font-semibold tracking-tight text-slate-950">
                Delete field?
              </h3>
              <p className="mt-2 text-sm leading-7 text-slate-600">
                This will permanently delete <span className="font-semibold text-slate-950">{pendingDeleteField.name}</span>
                {fieldTable ? (
                  <>
                    {" "}from <span className="font-semibold text-slate-950">{fieldTable.name}</span>.
                  </>
                ) : (
                  "."
                )}{" "}
                If links or dependencies exist, the backend may block the delete.
              </p>
            </div>
            {fieldFormError ? (
              <div className="mx-6 mt-5 rounded-md border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">
                {fieldFormError}
              </div>
            ) : null}
            <div className="flex items-center justify-end gap-3 px-6 py-5">
              <Button
                variant="outline"
                type="button"
                onClick={closeDeleteFieldModal}
                className="h-9 rounded-lg border-slate-200 px-4 text-sm shadow-none hover:translate-y-0"
              >
                Cancel
              </Button>
              <Button
                type="button"
                onClick={handleDeleteField}
                disabled={deleteFieldMutation.isPending}
                className="h-9 rounded-lg bg-red-600 px-4 text-sm text-white shadow-none hover:translate-y-0 hover:bg-red-700"
              >
                {deleteFieldMutation.isPending ? "Deleting..." : "Delete field"}
              </Button>
            </div>
          </div>
        </div>,
        document.body
      )
        : null}
    </>
  );
}
