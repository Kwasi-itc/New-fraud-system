"use client";

import { Button } from "@/components/ui/button";

export type IdentifierInput = { type: string; value: string; country?: string; issuer?: string };
const types = [["passport","Passport"],["national_id","National ID"],["registration","Company registration"],["tax_id","Tax ID"],["lei","LEI"],["imo","IMO number"]];
const control = "w-full rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm";

export function IdentifierFields({ rows, onChange, fields }: { rows: IdentifierInput[]; onChange: (rows: IdentifierInput[]) => void; fields?: string[] }) {
  const change = (index: number, key: keyof IdentifierInput, value: string) => onChange(rows.map((row,i) => i === index ? {...row,[key]:value} : row));
  return <fieldset className="space-y-3 rounded-xl border border-slate-200 p-4"><legend className="px-1 text-sm font-medium">Identifiers</legend>
    {rows.map((row,index) => <div key={index} className="grid gap-3 border-b border-slate-100 pb-3 sm:grid-cols-2">
      <label className="space-y-1 text-sm">Identifier type<select aria-label={`Identifier type ${index+1}`} className={control} value={row.type} onChange={e => change(index,"type",e.target.value)}>{types.map(([value,label]) => <option key={value} value={value}>{label}</option>)}</select></label>
      <label className="space-y-1 text-sm">{fields ? "Number field" : "Number"}{fields ? <select aria-label={`Identifier number field ${index+1}`} required className={control} value={row.value} onChange={e => change(index,"value",e.target.value)}><option value="">Choose a text field</option>{fields.map(value => <option key={value}>{value}</option>)}</select> : <input aria-label={`Identifier number ${index+1}`} required className={control} value={row.value} maxLength={200} onChange={e => change(index,"value",e.target.value)} />}</label>
      <label className="space-y-1 text-sm">{fields ? "Issuing country field" : "Issuing country"}{fields ? <select aria-label={`Identifier country field ${index+1}`} className={control} value={row.country ?? ""} onChange={e => change(index,"country",e.target.value)}><option value="">Not included</option>{fields.map(value => <option key={value}>{value}</option>)}</select> : <input aria-label={`Identifier country ${index+1}`} className={control} value={row.country ?? ""} maxLength={2} placeholder="GH" onChange={e => change(index,"country",e.target.value)} />}</label>
      <label className="space-y-1 text-sm">Issuer<input aria-label={`Identifier issuer ${index+1}`} className={control} value={row.issuer ?? ""} maxLength={200} onChange={e => change(index,"issuer",e.target.value)} /></label>
      <Button type="button" variant="outline" onClick={() => onChange(rows.filter((_,i) => i !== index))}>Remove identifier {index+1}</Button>
    </div>)}
    <p className="text-xs text-slate-500">Use the issuing country or issuer for local IDs. Keep leading zeros and punctuation. Exact agreement creates a candidate for review.</p>
    <Button type="button" variant="outline" disabled={rows.length >= 5} onClick={() => onChange([...rows,{type:"passport",value:""}])}>Add identifier</Button>
  </fieldset>;
}
