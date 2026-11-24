"use client";

import { useId, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { identityRequest, type IdentityRole } from "@/lib/identity-api";
import { identityRoleLabels } from "@/lib/identity-roles";
import { AccountNotice, styles } from "./account/account-ui";

export function IdentityRolePicker({ selected, onChange, disabled = false, selectedName, label = "Role", onRoleSelected, scope = "" }: { selected: string[]; onChange: (roles: string[]) => void; disabled?: boolean; selectedName?: string; label?: string; onRoleSelected?: (role: IdentityRole | null) => void; scope?: string }) {
  const [after, setAfter] = useState("");
  const id = useId();
  const query = useQuery({ queryKey: ["identity-role-options", scope, after], queryFn: ({ signal }) => identityRequest<{ roles: IdentityRole[] }>(`roles?after=${encodeURIComponent(after)}`, { signal }) });
  const roles = query.data?.roles.slice(0, 100) ?? [], next = (query.data?.roles.length ?? 0) > 100 ? roles[99].id : "";
  const current = selected[0] ?? "";
  const options = new Map(roles.map(role => [role.id, role.name]));
  if (current && !options.has(current)) options.set(current, selectedName ?? identityRoleLabels[current] ?? current);
  return <div className={styles.field}>
    <label htmlFor={id}>{label}</label>
    {query.isError && <AccountNotice>{query.error.message}</AccountNotice>}
    <select id={id} className={styles.select} value={current} disabled={disabled || query.isPending || query.isError} onChange={event => { const value = event.target.value; onChange(value ? [value] : []); onRoleSelected?.(roles.find(role => role.id === value) ?? null); }}>
      <option value="">{query.isPending ? "Loading roles…" : "Choose a role…"}</option>
      {[...options].map(([value, name]) => <option value={value} key={value}>{name}</option>)}
    </select>
    {(after || next) && <div className={styles.row}><button className={styles.link} type="button" disabled={!after || disabled} onClick={() => setAfter("")}>First role options</button><button className={styles.link} type="button" disabled={!next || disabled} onClick={() => setAfter(next)}>Next role options</button></div>}
    {query.isError && <button className={styles.link} type="button" onClick={() => void query.refetch()}>Retry role options</button>}
  </div>;
}
