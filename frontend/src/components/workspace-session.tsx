"use client";

import { createContext, useContext, useEffect, useState, type ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useDataModelWorkspaceStore } from "@/stores/data-model-store";
import { resetDecisionPages } from "@/lib/decision-pages";

const TenantContext = createContext("");
export const useWorkspaceTenant = () => useContext(TenantContext);

// The authenticated layout keys this provider by session/version/tenant. Its
// query cache is discarded on an identity change rather than shared across users.
export function WorkspaceSession({ tenant, scope, children }: { tenant: string; scope: string; children: ReactNode }) {
  const [client] = useState(() => new QueryClient({ defaultOptions: { queries: { staleTime: 30000, retry: 1, refetchOnWindowFocus: false } } }));
  const boundScope = useDataModelWorkspaceStore(state => state.sessionScope);
  useEffect(() => { resetDecisionPages(scope); useDataModelWorkspaceStore.getState().setTenantId(tenant, scope); }, [tenant, scope]);
  return <TenantContext.Provider value={tenant}><QueryClientProvider client={client}>
    {boundScope === scope ? children : <p role="status" className="p-6">Loading your workspace…</p>}
  </QueryClientProvider></TenantContext.Provider>;
}
