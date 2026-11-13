"use client";

import { create } from "zustand";

export type DataModelView = "data-model" | "schema" | "viewer";

type DataModelWorkspaceState = {
  tenantId: string;
  activeView: DataModelView;
  selectedTableId: string | null;
  setTenantId: (tenantId: string) => void;
  setActiveView: (view: DataModelView) => void;
  setSelectedTableId: (tableId: string | null) => void;
};


export const useDataModelWorkspaceStore = create<DataModelWorkspaceState>((set) => ({
  tenantId: "",
  activeView: "data-model",
  selectedTableId: null,
  setTenantId: (tenantId) => set({ tenantId }),
  setActiveView: (activeView) => set({ activeView }),
  setSelectedTableId: (selectedTableId) => set({ selectedTableId }),
}));
