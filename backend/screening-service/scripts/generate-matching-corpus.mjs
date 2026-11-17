// Offline reproducible engineering corpus. No people/downloads/AI labels.
// Transform labels require independent human adjudication before risk acceptance.
import { writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
const words=["Avelin","Bramora","Cyleno","Darevik","Elvona","Farsin","Galvero","Hesora","Iveran","Jalven","Kelora","Lornik","Mavora","Neral","Ostiven","Pelnora","Quelin","Ravena","Sorevin","Talora","Uverin","Valtero","Wenora","Xalven","Yorvik","Zelnora","Adrelin","Belvona","Coriven","Dalmora","Evaren","Felnora","Gorvik","Helvona","Istran","Jorvena","Kalmora","Liveran","Morvena","Nostrel","Orvona","Pavorin","Quorena","Rostiven","Selvona","Torven","Ulvena","Vostrel","Worena","Xorvik","Yelvona","Zorven","Almora","Bostren","Caldra","Devora","Elnora","Fovena","Galmir","Horvena","Islora","Jovaren","Korven","Lestora"];
const strata=["exact","alias","minor_spelling","significant_spelling","partial_name","word_order","diacritic","transliteration_alias","identifier_evidence","entity_type"];
let seed=20261114;const rnd=()=>{seed=(Math.imul(seed,1664525)+1013904223)>>>0;return seed};
const records=[],queries=[],seen=new Set();
for(let i=0;i<200;i++){
 const family=`family-${String(i).padStart(3,"0")}`,partition=i<60?"development":i<100?"calibration":"holdout",type=i%4<2?"Person":"Organization",source=i%3===0?"quality-global":"quality-private";
 let parts;do{parts=Array.from({length:4},()=>words[(rnd()>>>8)%words.length]);if(type==="Organization")parts[3]=["Finance","Systems","Partners","Holdings"][(rnd()>>>8)%4];}while(seen.has(parts.join(" ")));seen.add(parts.join(" "));
 const name=parts.join(" "),alias=`${parts[0]} ${parts[2]}`,roman=`${parts[1]} ${parts[0]} ${parts[2]} ${parts[3]}`;
 // An explicit source alias stands in for cross-script acquisition. The engine
 // does not infer transliterations; arbitrary scripts are not claimed supported.
 const native=`${i%2?"Альвена":"Αλβένα"} ${parts.slice(1).join(" ")}`;
 const properties=type==="Person"?{birthDate:[`${1960+i%40}-05-12`],nationality:["gh"]}:{registrationNumber:[`000${7000+i}`],country:["gh"]};
 const record={id:family,name,type,aliases:[alias,roman,native],properties,source_reference:"Fictional engineering corpus; not an official designation"};
 records.push({family,partition,source,category:source==="quality-global"?"controlled_fixture":"tenant_watchlist",record});
 const alter=(value,n)=>{const chars=[...value];for(let j=0;j<n;j++){let at=2+j*5;if(at>=chars.length)at=chars.length-2;if(chars[at]!==" ")chars[at]=chars[at]==="v"?"f":"v";}return chars.join("")};
 for(let j=0;j<5;j++){
  const k=(i+j*2)%10,stratum=strata[k];let positive=name;
  if(k===1)positive=alias;if(k===2)positive=alter(name,1);if(k===3)positive=alter(name,3);if(k===4)positive=parts.slice(0,3).join(" ");if(k===5)positive=[...parts].reverse().join(" ");if(k===6)positive=name.replace(/[aeio]/i,v=>({a:"á",e:"é",i:"í",o:"ó"})[v.toLowerCase()]);if(k===7)positive=native;
  const q={name:positive,type};if(k===8){if(type==="Person")q.birth_date=properties.birthDate[0];else q.registration_number=properties.registrationNumber[0];}
  const common={family,partition,source,category:source==="quality-global"?"controlled_fixture":"tenant_watchlist",entity_type:type,stratum};
  queries.push({...common,id:`${family}-positive-${j}`,query:q,relevant:[family],rationale:`Known source identity via ${stratum} transformation`});
  let negative={name:`${parts[0]} ${words[(i+23)%words.length]} ${words[(i+41)%words.length]}`,type};
  if(k===0)negative={name:alter(name,5),type};
  if(k===1)negative={name:`${parts[0]} ${words[(i+13)%words.length]}`,type};
  if(k===2)negative={name:`${parts[0]} ${parts[1]} ${words[(i+29)%words.length]} ${words[(i+7)%words.length]}`,type};
  if(k===4)negative={name:parts[0],type};
  if(k===8){negative={name,type};if(type==="Person")negative.birth_date="1901-01-01";else negative.registration_number=`different-${i}`;}
  if(k===9)negative={name,type:type==="Person"?"Organization":"Person"};
  queries.push({...common,id:`${family}-negative-${j}`,query:negative,relevant:[],rationale:k===8?"Conflicting source identifier/date":k===9?"Wrong entity type":"Similar but independently labelled unrelated synthetic identity"});
 }
}
const out={version:1,seed:20261114,label_method:"Deterministic engineering transformations; independent human adjudication pending",limitations:["200 synthetic collision families; query count is not independent sample size","No official-source quality or universal transliteration claim","Holdout is separated by identity family; no two-reviewer adjudication has occurred"],records,queries};
const destination=fileURLToPath(new URL("../testdata/matching-corpus-v1.json",import.meta.url));writeFileSync(destination,JSON.stringify(out,null,2)+"\n");
console.log(JSON.stringify({records:records.length,queries:queries.length,partitions:Object.fromEntries(["development","calibration","holdout"].map(p=>[p,queries.filter(q=>q.partition===p).length]))}));
