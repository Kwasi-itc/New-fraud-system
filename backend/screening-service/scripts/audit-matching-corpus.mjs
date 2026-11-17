// Read-only audit: never relabels, removes or regenerates the frozen corpus.
import { existsSync, readFileSync, realpathSync, writeFileSync } from 'node:fs';
import { createHash } from 'node:crypto';
import { fileURLToPath } from 'node:url';
import { basename, dirname, isAbsolute, join, relative, resolve, sep } from 'node:path';
const file=fileURLToPath(new URL('../testdata/matching-corpus-v2.json',import.meta.url));
const bytes=readFileSync(file), corpus=JSON.parse(bytes);
const byID=new Map(corpus.records.map(r=>[r.record.id,r]));
const key=s=>s.normalize('NFKC').toLowerCase().trim().replace(/\s+/gu,' ');
const nameIndex=new Map(), requests=new Map();
const add=(map,k,v)=>{const a=map.get(k)??[];a.push(v);map.set(k,a)};
for(const r of corpus.records)for(const name of new Set([r.record.name,...r.record.aliases??[]]))add(nameIndex,`${r.record.type}:${key(name)}`,r);
const strata={}, examples=[];
let negativeExactCandidates=0, positiveSharedNames=0, invalidReferences=0, queriesWithNoID=0;
for(const q of corpus.queries){
  const canonical=JSON.stringify(Object.fromEntries(Object.entries(q.query).sort(([a],[b])=>a.localeCompare(b))));
  add(requests,canonical,q);
  const s=strata[q.stratum]??={queries:0,negative_exact_name_candidates:0,positive_shared_exact_names:0};s.queries++;
  for(const id of q.relevant)if(!byID.has(id))invalidReferences++;
  if(!q.query.identifiers?.length&&!q.query.registration_number&&!q.query.birth_date)queriesWithNoID++;
  const visible=(nameIndex.get(`${q.query.type}:${key(q.query.name)}`)??[]).filter(r=>{
    for(const [field,property]of [['birth_date','birthDate'],['registration_number','registrationNumber']]){
      const values=r.record.properties?.[property];
      if(q.query[field]&&values?.length&&!values.some(v=>key(v)===key(q.query[field])))return false;
    }
    return true;
  });
  const unique=[...new Set(visible.map(r=>r.record.id))];
  if(!q.indeterminate&&!q.relevant.length&&unique.length){negativeExactCandidates++;s.negative_exact_name_candidates++;if(examples.length<12)examples.push({query_id:q.id,partition:q.partition,kind:'labelled-negative-with-observable-exact-source-name',candidate_ids:unique});}
  if(!q.indeterminate&&q.relevant.length&&unique.some(id=>!q.relevant.includes(id))){positiveSharedNames++;s.positive_shared_exact_names++;}
}
const conflicts=[...requests.values()].filter(a=>new Set(a.filter(q=>!q.indeterminate).map(q=>JSON.stringify([...q.relevant].sort()))).size>1);
const crossPartition=[...requests.values()].filter(a=>new Set(a.map(q=>q.partition)).size>1);
const collisions=[...nameIndex.entries()].filter(([,a])=>new Set(a.map(r=>r.record.id)).size>1);
const typeCategories={};for(const r of corpus.records){const k=r.category;const m=typeCategories[k]??={};m[r.record.type]=(m[r.record.type]??0)+1;}
const report={corpus_sha256:createHash('sha256').update(bytes).digest('hex'),records:corpus.records.length,queries:corpus.queries.length,invalid_relevant_references:invalidReferences,identical_input_groups_with_conflicting_determinate_labels:conflicts.length,queries_in_conflicting_input_groups:conflicts.reduce((n,a)=>n+a.length,0),identical_input_groups_crossing_partitions:crossPartition.length,shared_exact_name_or_alias_groups:collisions.length,name_groups_crossing_family_partitions:collisions.filter(([,a])=>new Set(a.map(r=>r.partition)).size>1).length,determinate_negative_queries_with_nonconflicting_exact_catalog_names:negativeExactCandidates,determinate_positive_queries_sharing_exact_catalog_names:positiveSharedNames,queries_without_identifier_or_date:queriesWithNoID,source_labels:[...new Set(corpus.records.map(r=>r.source))],entity_by_category:typeCategories,by_stratum:strata,examples,method:'Exact duplicate request keys use all raw query fields and a fixed selection. Name collision diagnostics use NFKC lowercase/whitespace only, not the production scoring normalizer. Source/category labels are fictional strata, not official-source acceptance.',label_method:corpus.label_method,limitations:['A different fictional identity does not make an overlapping name an observable true negative. Candidate relevance and legal identity truth require different annotations.','Families split by ordinal still share aliases, name tokens and identical query inputs; query-level Wilson intervals do not establish independent acceptance.','No labels, holdout cases, thresholds or frozen scorer files were changed.','No numeric acceptance claim is inferred from this corpus audit.'],quality_accepted:false};
if(process.argv[2]) {
  const output=resolve(process.argv[2]);
  const canonical=existsSync(output)?realpathSync(output):join(realpathSync(dirname(output)),basename(output));
  for(const root of ['../testdata/','../internal/matching/'].map(p=>realpathSync(fileURLToPath(new URL(p,import.meta.url))))) {
    const inside=relative(root,canonical);
    if(inside===''||(!inside.startsWith('..'+sep)&&inside!=='..'&&!isAbsolute(inside)))throw new Error('Report output must not overwrite matching code or test data');
  }
  writeFileSync(output,JSON.stringify(report,null,2)+'\n');
}
else process.stdout.write(JSON.stringify(report,null,2)+'\n');
