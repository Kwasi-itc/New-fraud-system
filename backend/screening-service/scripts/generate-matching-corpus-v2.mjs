// A second, independently generated engineering corpus. v1 remains unchanged.
// Fictional identities and transformation labels require independent reviewers.
import { writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
const first=["Ama","Kojo","Akosua","Kofi","Amina","Yara","Nadia","Sofia","Mikhail","Daria","Emeka","Chika","Farah","Omar","Zain","Mei","Jian","Aruna","Sahana","Nimal","Elena","Mateo","Ines","Luis"];
const last=["Mensori","Owanza","Tavani","Al Nouri","Terekova","Zelenin","Nwero","Okembe","Ben Zahri","El Hadi","Linwei","Qianru","Peravani","Jayatene","Velenzo","Montevia","Osei Dara","Nartevo","Kobina Mensori","Adu Tavani","Da Velenzo","Al Owanza","Noor Tavani","Lin Mensori"];
const company=["Cedar","Aurora","Cobalt","Orchid","Silver","Coral","Maple","Saffron","Juniper","Meridian","Willow","Quartz","Harbor","Aspen","Tamarind","Lotus","Bamboo","Violet","Acacia","Horizon","Indigo","Pine","Olive","Citrine"];
const category=["synthetic_sanctions","synthetic_pep_evidence","synthetic_regulatory_warning","tenant_watchlist"];
const strata=["exact","alias","minor_spelling","significant_spelling","partial_name","word_order","diacritic","transliteration_alias","identifier_evidence","entity_type"];
let seed=2026111402;const random=()=>{seed=(Math.imul(seed,1664525)+1013904223)>>>0;return seed;};
const choose=a=>a[random()%a.length];const records=[],queries=[],seen=new Set();
const typo=(s,count)=>{const chars=[...s];const letters=chars.map((v,i)=>/[A-Za-z]/.test(v)?i:-1).filter(i=>i>=0);for(let j=0;j<count;j++){const i=letters[(j*7+2)%letters.length];chars[i]=chars[i].toLowerCase()==="z"?"s":"z";}return chars.join("");};
for(let i=0;i<240;i++){
 const family=`v2-family-${String(i).padStart(3,"0")}`,partition=i<72?"development":i<120?"calibration":"holdout",type=i%2?"Organization":"Person",source=i%3?"quality-private":"quality-global";
 let name;do{name=type==="Person"?`${choose(first)} ${i%4===0?choose(first)+" ":""}${choose(last)}`:`${choose(company)} ${choose(company)} ${["Finance","Logistics","Systems","Holdings"][i%4]}`;}while(seen.has(name));seen.add(name);
 const parts=name.split(" "),alias=type==="Person"?`${parts.at(-1)} ${parts.slice(0,-1).join(" ")}`:`${parts.slice(0,-1).join(" ")} Company`;
 const native=`${i%4===0?"Альвена":i%4===1?"Αλβένα":i%4===2?"أمينة":"美兰"} ${parts.at(-1)}`;
 const props=type==="Person"?{birthDate:[`${1950+i%60}-${String(i%12+1).padStart(2,"0")}-12`],nationality:[i%3?"gh":"gb"]}:{registrationNumber:[`00${100000+i}`],country:[i%3?"gh":"gb"]};
 records.push({family,partition,source,category:category[i%4],record:{id:family,name,type,aliases:[alias,native],properties:props,source_reference:"Fictional expanded engineering corpus; not an official designation"}});
 for(let k=0;k<10;k++){
  let positive=name,indeterminate=false;
  if(k===1)positive=alias;if(k===2)positive=typo(name,1);if(k===3)positive=typo(name,3);
  if(k===4){positive=parts.slice(0,Math.max(1,parts.length-1)).join(" ");indeterminate=positive.split(" ").length<2;}
  if(k===5)positive=[...parts].reverse().join(" ");if(k===6)positive=name.replace(/[aeio]/i,v=>({a:"á",e:"é",i:"í",o:"ó"})[v.toLowerCase()]);if(k===7)positive=native;
  let q={name:positive,type};if(k===8){if(type==="Person")q.birth_date=props.birthDate[0];else q.registration_number=props.registrationNumber[0];}
  const common={family,partition,source,category:category[i%4],entity_type:type,stratum:strata[k]};
  queries.push({...common,id:`${family}-positive-${k}`,query:q,relevant:[family],indeterminate,rationale:indeterminate?"Under-specified partial name; not counted as a correct positive or negative":"Known fictional source identity via declared transformation"});
  let nq={name:k%2?`${parts[0]} ${choose(last)}`:typo(name,5),type};let ambiguous=false;
  if(k===0&&i%5===0){nq={name,type};ambiguous=true;}
  if(k===4)nq={name:parts[0],type};
  if(k===8){nq={name,type};if(type==="Person")nq.birth_date="1901-01-01";else nq.registration_number=`00-conflicting-${i}`;}
  if(k===9)nq={name,type:type==="Person"?"Organization":"Person"};
  queries.push({...common,id:`${family}-negative-${k}`,query:nq,relevant:[],indeterminate:ambiguous,rationale:ambiguous?"Unrelated identical-name identity without corroboration; indeterminate from these query fields":"Independent fictional identity/type or explicit identifier/date conflict"});
 }
}
const out={version:2,seed:2026111402,label_method:"Deterministic engineering transformations; independent human adjudication pending",limitations:["No official-source or production accuracy claim","240 collision families; query rows are correlated","Indeterminate cases are counted separately, not correct no-hits","The previously observed v1 corpus remains a regression set; this is a new family split"],records,queries};
writeFileSync(fileURLToPath(new URL("../testdata/matching-corpus-v2.json",import.meta.url)),JSON.stringify(out,null,2)+"\n");
console.log(JSON.stringify({records:records.length,queries:queries.length,indeterminate:queries.filter(q=>q.indeterminate).length}));
