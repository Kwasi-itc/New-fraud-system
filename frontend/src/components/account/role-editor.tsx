"use client";

import { useEffect, useMemo, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { identityRequest, type IdentityCapability, type IdentityRole } from "@/lib/identity-api";
import { IdentityRolePicker } from "@/components/identity-role-picker";
import { AccountCard, AccountFrame, AccountModal, AccountNotice, styles } from "./account-ui";
import { capabilityArea, capabilityAreas, capabilityTitle } from "./account-capabilities";

export function IdentityRoleEditor({ roleId, capabilities, scope }: { roleId?: string; capabilities: string[]; scope: string }) {
  const catalog = useQuery({ queryKey: ["identity-capabilities", scope], queryFn: ({ signal }) => identityRequest<{ capabilities: IdentityCapability[] }>("capabilities", { signal }) });
  const role = useQuery({ queryKey: ["identity-role", scope, roleId], queryFn: ({ signal }) => identityRequest<IdentityRole>(`roles/${encodeURIComponent(roleId!)}`, { signal }), enabled: !!roleId });
  const title = roleId ? "Edit role" : "Create role";
  if (catalog.isPending || roleId && role.isPending) return <AccountFrame title={title} description="Loading role capabilities…" capabilities={capabilities}><p className={styles.empty}>Loading role editor…</p></AccountFrame>;
  if (catalog.isError || roleId && role.isError) return <AccountFrame title={title} description="Role details could not be loaded." capabilities={capabilities}><AccountNotice>{catalog.error?.message ?? role.error?.message}</AccountNotice><Link className={styles.link} href="/administration/roles">All roles</Link></AccountFrame>;
  if (role.data?.builtin) return <AccountFrame title={role.data.name} description="Predefined role" capabilities={capabilities}><AccountNotice kind="info">Predefined roles are read-only. Create a custom role and copy its allowed capabilities to use it as a starting point.</AccountNotice><Link className={styles.link} href="/administration/roles">All roles</Link></AccountFrame>;
  return <RoleEditorForm key={roleId ?? "new-role"} role={role.data} catalog={catalog.data!.capabilities.filter(cap => cap.human)} capabilities={capabilities} scope={scope} />;
}

function RoleEditorForm({ role, catalog, capabilities, scope }: { role?: IdentityRole; catalog: IdentityCapability[]; capabilities: string[]; scope: string }) {
  const router = useRouter(), queryClient = useQueryClient();
  const [name, setName] = useState(role?.name ?? ""), [selected, setSelected] = useState(new Set(role?.capabilities ?? []));
  const [area, setArea] = useState("all"), [search, setSearch] = useState(""), [selectedOnly, setSelectedOnly] = useState(false);
  const [source, setSource] = useState<IdentityRole | null>(null), [sourceIds, setSourceIds] = useState<string[]>([]);
  const [busy, setBusy] = useState(false), [error, setError] = useState(""), [notice, setNotice] = useState(""), [leave, setLeave] = useState<string | null>(null);
  const catalogByName = useMemo(() => new Map(catalog.map(cap => [cap.name, cap])), [catalog]);
  const dirty = name !== (role?.name ?? "") || JSON.stringify([...selected].sort()) !== JSON.stringify([...(role?.capabilities ?? [])].sort());
  const visible = catalog.filter(cap => (area === "all" || capabilityArea(cap.name).id === area) && (!selectedOnly || selected.has(cap.name)) && `${cap.name} ${capabilityTitle(cap)} ${capabilityArea(cap.name).name}`.toLowerCase().includes(search.toLowerCase()));
  useEffect(() => { if (!dirty) return; const warn = (event: BeforeUnloadEvent) => { event.preventDefault(); }; window.addEventListener("beforeunload", warn); return () => window.removeEventListener("beforeunload", warn); }, [dirty]);
  function navigate(href: string) { if (dirty) setLeave(href); else router.push(href); }
  function changeCapability(cap: IdentityCapability, checked: boolean) { if (cap.reserved || busy) return; setSelected(previous => { const next = new Set(previous); if (checked) next.add(cap.name); else next.delete(cap.name); return next; }); }
  function selectVisible(checked: boolean) { setSelected(previous => { const next = new Set(previous); for (const cap of visible) if (!cap.reserved) { if (checked) next.add(cap.name); else next.delete(cap.name); } return next; }); }
  function copySource() {
    if (!source) return;
    const allowed = source.capabilities.filter(value => catalogByName.has(value) && !catalogByName.get(value)!.reserved);
    setSelected(previous => new Set([...previous, ...allowed])); setNotice(`Added allowed capabilities from ${source.name}. This role keeps an independent capability bundle.`);
  }
  async function save() {
    setError("");
    const trimmed = name.trim();
    if (!trimmed || new TextEncoder().encode(trimmed).length > 100) { setError("Role name must be 1–100 bytes."); return; }
    if (!selected.size) { setError("Choose at least one capability for this role."); return; }
    if ([...selected].some(value => !catalogByName.has(value) || catalogByName.get(value)!.reserved)) { setError("Reserved or unknown capabilities cannot be included in a custom role."); return; }
    setBusy(true);
    try {
      const saved = await identityRequest<IdentityRole>(role ? `roles/${role.id}` : "roles", { method: role ? "PUT" : "POST", body: JSON.stringify({ name: trimmed, capabilities: [...selected].sort(), version: role?.version ?? 0 }) });
      await queryClient.invalidateQueries({ queryKey: ["identity-roles", scope] });
      await queryClient.invalidateQueries({ queryKey: ["identity-role", scope, saved.id] });
      await queryClient.invalidateQueries({ queryKey: ["identity-role-options", scope] });
      router.push(`/administration/roles?role=${encodeURIComponent(saved.id)}`); router.refresh();
    } catch (reason) { setError(reason instanceof Error ? reason.message : "Role could not be saved. Refresh before retrying a conflicting edit."); setBusy(false); }
  }
  const saveButton = <button type="submit" form="role-editor" className={`${styles.button} ${styles.primary}`} disabled={busy}>{busy ? "Saving…" : role ? "Save changes" : "Create role"}</button>;
  return <AccountFrame title={role ? "Edit role" : "Create role"} description="Choose the capabilities this role grants in this tenant." capabilities={capabilities} requestNavigation={navigate} action={<div className={styles.row}><button className={styles.button} disabled={busy} onClick={() => navigate("/administration/roles")}>Cancel</button>{saveButton}</div>}>
    <button className={styles.link} style={{ marginBottom: 18 }} disabled={busy} onClick={() => navigate("/administration/roles")}>All roles</button>
    {error && <AccountNotice>{error}</AccountNotice>}{notice && <AccountNotice kind="info">{notice}</AccountNotice>}
    <form id="role-editor" className={styles.stack} onSubmit={event => { event.preventDefault(); void save(); }}><AccountCard><div className={styles.editorTop}>
      <div className={styles.field}><label htmlFor="role-name">Role name</label><input id="role-name" className={styles.input} required maxLength={100} value={name} disabled={busy} placeholder="e.g. Payments review team" onChange={event => setName(event.target.value)} /><p className={styles.hint}>Choose a name that explains what this role is for.</p></div>
      <div className={styles.stack} style={{ gap: 10 }}><IdentityRolePicker label="Add capabilities from a role" selected={sourceIds} onChange={value => { setSourceIds(value); if (!value.length) setSource(null); }} onRoleSelected={setSource} disabled={busy} scope={scope} /><button className={styles.button} type="button" disabled={busy || !source} onClick={copySource}>Add capabilities</button><p className={styles.hint}>Adds a copy of its allowed capabilities. Adjust the selection below.</p></div>
    </div></AccountCard><div className={styles.editorGrid}>
      <aside className={`${styles.card} ${styles.editorSide}`}><div className={styles.cardHeader}><h2>Capability areas</h2></div><div className={styles.categories}>{capabilityAreas.map(group => { const values = catalog.filter(cap => group.id === "all" || capabilityArea(cap.name).id === group.id); return <button type="button" className={styles.category} key={group.id} aria-pressed={area === group.id} disabled={busy} onClick={() => setArea(group.id)}><span>{group.name}</span><span>{values.filter(cap => selected.has(cap.name)).length}/{values.length}</span></button>; })}</div></aside>
      <AccountCard><div className={`${styles.stack} ${styles.cardBody}`} style={{ gap: 14 }}><input className={styles.input} type="search" aria-label="Search capabilities" placeholder="Search capabilities…" value={search} onChange={event => setSearch(event.target.value)} onKeyDown={event => { if (event.key === "Enter") event.preventDefault(); }} /><div className={`${styles.row} ${styles.between} ${styles.wrap}`}><label className={`${styles.row} ${styles.small}`}><input type="checkbox" checked={selectedOnly} onChange={event => setSelectedOnly(event.target.checked)} />Selected only</label><div className={styles.row}><button className={styles.link} type="button" disabled={busy} onClick={() => selectVisible(true)}>Select visible</button><button className={styles.link} type="button" disabled={busy} onClick={() => selectVisible(false)}>Clear visible</button></div></div></div>
        {!visible.length && <p className={styles.empty}>No capabilities match these filters.</p>}
        {capabilityAreas.slice(1).map(group => { const values = visible.filter(cap => capabilityArea(cap.name).id === group.id); return values.length ? <section key={group.id} aria-label={group.name}><div className={styles.groupHeading}><h3>{group.name}</h3><span>{values.length}</span></div>{values.map(cap => <label className={styles.capabilityRow} key={cap.name}><input type="checkbox" aria-label={cap.name} checked={selected.has(cap.name)} disabled={busy || cap.reserved} onChange={event => changeCapability(cap, event.target.checked)} /><span className={styles.capabilityText}><strong>{capabilityTitle(cap)}</strong><code>{cap.name}</code></span>{cap.reserved && <span className={`${styles.badge} ${styles.gray}`}>Tenant Admin only</span>}</label>)}</section> : null; })}
      </AccountCard>
      <aside className={`${styles.card} ${styles.editorSide}`}><div className={styles.cardBody}><h2 className={styles.cellTitle}>Selection summary</h2><p className={styles.summaryCount}>{selected.size}</p><p className={styles.muted}>capabilities selected</p><p className={styles.description}>{name.trim() || "Untitled role"}</p><div className={styles.summaryList}>{[...selected].sort().map(value => <code key={value}>{value}</code>)}</div><p className={styles.hint} style={{ marginTop: 20 }}>Role creation and assignment stay with Tenant Administrators.</p></div></aside>
    </div><div className={`${styles.row} ${styles.between} ${styles.wrap}`}><span className={styles.hint}>{catalog.filter(cap => !cap.reserved).length} assignable capabilities · {catalog.filter(cap => cap.reserved).length} reserved</span><div className={styles.row}><button type="button" className={styles.button} disabled={busy} onClick={() => navigate("/administration/roles")}>Cancel</button>{saveButton}</div></div></form>
    {leave && <AccountModal title="Discard role changes?" onClose={() => setLeave(null)}><p className={styles.muted}>Your unsaved role changes will be discarded.</p><div className={styles.formActions}><button className={styles.button} onClick={() => setLeave(null)}>Keep editing</button><button className={`${styles.button} ${styles.danger}`} onClick={() => router.push(leave)}>Discard changes</button></div></AccountModal>}
  </AccountFrame>;
}
