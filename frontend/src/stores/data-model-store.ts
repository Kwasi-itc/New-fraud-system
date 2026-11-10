"use client";

import { create } from "zustand";

export type DataModelView = "data-model" | "schema" | "viewer";

type DataModelWorkspaceState = {
  tenantId: string;
  sessionScope: string;
  activeView: DataModelView;
  selectedTableId: string | null;
  setTenantId: (tenantId: string, sessionScope?: string) => void;
  setActiveView: (view: DataModelView) => void;
  setSelectedTableId: (tableId: string | null) => void;
};


export const useDataModelWorkspaceStore = create<DataModelWorkspaceState>((set) => ({
  tenantId: "",
  sessionScope: "",
  activeView: "data-model",
  selectedTableId: null,
  setTenantId: (tenantId, sessionScope = "") => set(state => state.tenantId === tenantId && state.sessionScope === sessionScope ? state : { tenantId, sessionScope, selectedTableId: null, activeView: "data-model" }),
  setActiveView: (activeView) => set({ activeView }),
  setSelectedTableId: (selectedTableId) => set({ selectedTableId }),
}));
