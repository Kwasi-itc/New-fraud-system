"use client";

import { createContext, useContext, useEffect, useState, type ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useDataModelWorkspaceStore } from "@/stores/data-model-store";
import { resetDecisionPages } from "@/lib/decision-pages";

const TenantContext = createContext("");
export const useWorkspaceTenant = () => useContext(TenantContext);

// The authenticated layout keys this boundary by session, tenant and version.
export function WorkspaceSession({ tenant, children }: { tenant: string; children: ReactNode }) {
  const boundTenant = useDataModelWorkspaceStore(state => state.tenantId);
  const [client] = useState(() => new QueryClient({ defaultOptions: { queries: { staleTime: 30000, retry: 1, refetchOnWindowFocus: false } } }));
  useEffect(() => {
    resetDecisionPages(tenant);
    useDataModelWorkspaceStore.getState().setTenantId(tenant);
    useDataModelWorkspaceStore.getState().setSelectedTableId(null);
  }, [tenant]);
  return <TenantContext.Provider value={tenant}><QueryClientProvider client={client}>{boundTenant === tenant ? children : <p role="status" className="p-6">Loading your workspace…</p>}</QueryClientProvider></TenantContext.Provider>;
}
