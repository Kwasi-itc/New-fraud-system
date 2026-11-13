import Link from "next/link";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { screeningDescription, screeningOutcomeLabel, type ScreeningListItem } from "@/lib/scenario-screening";

export function ScenarioScreeningRow({ rule, scenarioId, canEdit, defaultThreshold }: {
  rule: ScreeningListItem;
  scenarioId: string;
  canEdit: boolean;
  defaultThreshold?: number;
}) {
  const href = `/detection/${scenarioId}/screening?configId=${encodeURIComponent(rule.id)}`;
  return <tr className="border-b border-slate-100 text-[14px] text-slate-950 last:border-b-0">
    <td className="px-4 py-3 font-medium"><Link href={href} className="hover:text-[#1f4f96]">{rule.name}</Link></td>
    <td className="px-4 py-3"><div className="flex flex-wrap gap-1.5">
      <Badge className="border-violet-200 bg-violet-50 text-violet-800 normal-case tracking-normal">Screening</Badge>
      <Badge className={`${rule.enabled ? "border-emerald-200 bg-emerald-50 text-emerald-800" : "border-slate-200 bg-slate-100 text-slate-600"} normal-case tracking-normal`}>{rule.enabled ? "Enabled" : "Disabled"}</Badge>
    </div></td>
    <td className="max-w-[440px] px-4 py-3 text-[13px]">{screeningDescription(rule, defaultThreshold)}</td>
    <td className="px-4 py-3 text-slate-400" aria-label="No rule group">—</td>
    <td className="px-4 py-3 text-center text-[13px] text-slate-500">No score change</td>
    <td className="px-4 py-3 text-[13px]">{rule.forced_outcome
      ? <><span className="block font-medium">At least {screeningOutcomeLabel(rule.forced_outcome)}</span><span className="text-xs text-slate-500">On a possible match</span></>
      : <><span className="block">After decision</span><span className="text-xs text-slate-500">{rule.outcomes.map(screeningOutcomeLabel).join(", ")}</span></>}</td>
    <td className="px-4 py-3 text-right"><Button asChild variant="outline" className="h-8 rounded-lg border-slate-200 px-3 text-[12px] shadow-none"><Link href={href}>{canEdit ? "Edit" : "View"}</Link></Button></td>
  </tr>;
}
