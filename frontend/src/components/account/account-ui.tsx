"use client";

import { useEffect, useId, useRef, type ReactNode } from "react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { X } from "lucide-react";
import styles from "./account.module.css";

export { styles };
export function AccountFrame({ title, description, capabilities, action, children, requestNavigation }: { title: string; description: string; capabilities: string[]; action?: ReactNode; children: ReactNode; requestNavigation?: (href: string) => void }) {
  const pathname = usePathname();
  const links = [
    { href: "/my-account", label: "My account", visible: true },
    { href: "/administration/users", label: "Users & invitations", visible: capabilities.includes("roles:assign") },
    { href: "/administration/roles", label: "Roles & capabilities", visible: capabilities.includes("roles:manage") },
    { href: "/administration/api-clients", label: "API clients", visible: capabilities.includes("clients:read") },
  ];
  return <div className={styles.area}>
    <header className={styles.header}><div><h1>{title}</h1><p className={styles.description}>{description}</p></div>{action}</header>
    <nav className={styles.tabs} aria-label="Account pages">{links.filter(link => link.visible).map(link => <Link key={link.href} href={link.href} onClick={event => { if (requestNavigation && !event.metaKey && !event.ctrlKey && !event.shiftKey && !event.altKey) { event.preventDefault(); requestNavigation(link.href); } }} className={styles.tab} aria-current={pathname === link.href || pathname.startsWith(link.href + "/") ? "page" : undefined}>{link.label}</Link>)}</nav>
    {children}
  </div>;
}
export function AccountCard({ title, description, action, children, className = "" }: { title?: ReactNode; description?: string; action?: ReactNode; children: ReactNode; className?: string }) {
  return <section className={`${styles.card} ${className}`}>{title && <header className={styles.cardHeader}><div><h2>{title}</h2>{description && <p className={styles.description}>{description}</p>}</div>{action}</header>}{children}</section>;
}
export function AccountNotice({ children, kind = "error" }: { children: ReactNode; kind?: "error" | "success" | "info" }) {
  return <p className={`${styles.notice} ${kind === "error" ? styles.error : kind === "success" ? styles.success : ""}`} role={kind === "error" ? "alert" : kind === "success" ? "status" : undefined}>{children}</p>;
}
export function AccountModal({ title, onClose, busy = false, children }: { title: string; onClose: () => void; busy?: boolean; children: ReactNode }) {
  const ref = useRef<HTMLDialogElement>(null), id = useId();
  useEffect(() => { const dialog = ref.current; if (!dialog) return; dialog.showModal(); return () => dialog.close(); }, []);
  return <dialog ref={ref} className={styles.dialog} aria-labelledby={id} onCancel={event => { event.preventDefault(); if (!busy) onClose(); }}>
    <header className={styles.dialogHeader}><h2 id={id}>{title}</h2><button type="button" className={styles.button} aria-label="Close dialog" disabled={busy} onClick={onClose}><X size={16} /></button></header>
    <div className={styles.dialogBody}>{children}</div>
  </dialog>;
}
export function AccountPagination({ after, next, busy, onChange, label = "Page" }: { after: string; next: string; busy?: boolean; onChange: (cursor: string) => void; label?: string }) {
  return <div className={styles.pagination}><span>{after ? `${label} after the selected cursor` : `First ${label.toLowerCase()}`}</span><div className={styles.row}><button type="button" className={styles.link} disabled={!after || busy} onClick={() => onChange("")}>First page</button><button type="button" className={styles.link} disabled={!next || busy} onClick={() => onChange(next)}>Next page</button></div></div>;
}
export function accountDate(value: string) {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? "Unavailable" : date.toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" });
}
