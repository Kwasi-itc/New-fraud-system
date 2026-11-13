"use client";

import Link from "next/link";
import { useEffect, useId, useRef, useState, type KeyboardEvent } from "react";
import { ChevronDown, Plus } from "lucide-react";
import { Button } from "@/components/ui/button";

export function ScenarioAddMenu({ scenarioId, canAddRule, creatingRule, onAddRule, onOpen }: {
  scenarioId: string;
  canAddRule: boolean;
  creatingRule: boolean;
  onAddRule: () => void;
  onOpen: () => void;
}) {
  const [open, setOpen] = useState(false);
  const id = useId();
  const root = useRef<HTMLDivElement>(null);
  const trigger = useRef<HTMLButtonElement>(null);
  const menu = useRef<HTMLDivElement>(null);
  const focusLast = useRef(false);
  const close = (restoreFocus = false) => {
    setOpen(false);
    if (restoreFocus) trigger.current?.focus();
  };
  const show = (last = false) => {
    focusLast.current = last;
    onOpen();
    setOpen(true);
  };
  useEffect(() => {
    if (!open) return;
    const items = menu.current?.querySelectorAll<HTMLElement>('[role="menuitem"]:not(:disabled)');
    items?.[focusLast.current ? items.length - 1 : 0]?.focus();
    const dismiss = (event: Event) => {
      if (event.target instanceof Node && !root.current?.contains(event.target)) setOpen(false);
    };
    document.addEventListener("pointerdown", dismiss);
    document.addEventListener("focusin", dismiss);
    return () => {
      document.removeEventListener("pointerdown", dismiss);
      document.removeEventListener("focusin", dismiss);
    };
  }, [open]);

  const navigate = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.key === "Escape") {
      event.preventDefault();
      close(true);
    } else if (event.key === "Tab") {
      // Restore the trigger before the browser advances to the next tab stop.
      close(true);
    } else if (["ArrowDown", "ArrowUp", "Home", "End"].includes(event.key)) {
      event.preventDefault();
      const items = Array.from(menu.current?.querySelectorAll<HTMLElement>('[role="menuitem"]:not(:disabled)') ?? []);
      const current = items.indexOf(document.activeElement as HTMLElement);
      const next = event.key === "Home" ? 0 : event.key === "End" ? items.length - 1 :
        (current + (event.key === "ArrowDown" ? 1 : -1) + items.length) % items.length;
      items[next]?.focus();
    }
  };

  return <div ref={root} className="relative">
    <Button ref={trigger} type="button" disabled={creatingRule} aria-haspopup="menu" aria-expanded={open}
      aria-controls={open ? id : undefined}
      onClick={() => open ? close() : show()}
      onKeyDown={event => {
        if (event.key === "ArrowDown" || event.key === "ArrowUp") {
          event.preventDefault();
          show(event.key === "ArrowUp");
        }
      }}
      className="h-9 rounded-xl bg-[#1f4f96] px-3.5 text-[13px] shadow-none hover:bg-[#163f79]">
      <Plus aria-hidden="true" className="size-4" />{creatingRule ? "Creating..." : "Add"}
      <ChevronDown aria-hidden="true" className="size-3.5" />
    </Button>
    {open && <div ref={menu} id={id} role="menu" aria-label="Add to scenario" onKeyDown={navigate}
      className="absolute right-0 top-full z-30 mt-2 w-[360px] max-w-[calc(100vw-4rem)] rounded-xl border border-slate-200 bg-white p-2 shadow-[0_18px_40px_rgba(15,23,42,0.12)]">
      <button type="button" role="menuitem" aria-label="Add a rule" aria-describedby={`${id}-rule`}
        disabled={!canAddRule} onClick={() => { close(true); onAddRule(); }}
        className="flex w-full items-center gap-3 rounded-lg px-3 py-4 text-left hover:bg-slate-50 focus:bg-slate-50 focus:outline-none disabled:cursor-not-allowed disabled:text-slate-400">
        <Plus aria-hidden="true" className="size-5 shrink-0" />
        <span><span className="block text-sm font-medium">Add a rule</span>
          <span id={`${id}-rule`} className="mt-1 block text-sm text-slate-500">{canAddRule ? "Add a rule to the scenario" : "Select or create a draft iteration to add a rule"}</span></span>
      </button>
      <Link role="menuitem" aria-label="Add a screening" aria-describedby={`${id}-screening`}
        href={`/detection/${encodeURIComponent(scenarioId)}/screening?create=1`} onClick={() => close()}
        className="flex items-center gap-3 rounded-lg px-3 py-4 text-left hover:bg-slate-50 focus:bg-slate-50 focus:outline-none">
        <Plus aria-hidden="true" className="size-5 shrink-0" />
        <span><span className="block text-sm font-medium">Add a screening</span>
          <span id={`${id}-screening`} className="mt-1 block text-sm text-slate-500">Add a screening to the scenario</span></span>
      </Link>
    </div>}
  </div>;
}
