import {writeFileSync} from 'node:fs';
import {fileURLToPath} from 'node:url';
export const rules=[
 {name:'High transaction amount',expression:'amount >= 10000',score:35},
 {name:'High supplied risk score',expression:'risk_score >= 80',score:40},
 {name:'New account making a large payment',expression:'customer_age_days < 7 and amount >= 1000',score:25},
 {name:'Foreign currency and merchant country',expression:'currency != "USD" and merchant_country != "US"',score:10},
 {name:'Large card-not-present payment',expression:'not card_present and amount >= 5000',score:20},
];
export const fields={amount:'float',currency:'string',merchant_country:'string',risk_score:'float',customer_age_days:'int',card_present:'bool'};
export const thresholds={review:20,block_and_review:50,decline:80};
// Independent business oracle: no evaluator/AST implementation is imported.
export function expected(f){
 const hit=[f.amount>=10000,f.risk_score>=80,f.customer_age_days<7&&f.amount>=1000,f.currency!=='USD'&&f.merchant_country!=='US',!f.card_present&&f.amount>=5000];
 const score=hit.reduce((sum,yes,i)=>sum+(yes?rules[i].score:0),0);
 return {score,outcome:score>=80?'decline':score>=50?'block_and_review':score>=20?'review':'approve',hit_rules:rules.filter((_,i)=>hit[i]).map(r=>r.name)};
}
const baseline={amount:100,currency:'USD',merchant_country:'US',risk_score:0,customer_age_days:30,card_present:true};
const named=[
 ['ordinary payment',{},0,'approve'],
 ['amount below threshold',{amount:9999.99},0,'approve'],
 ['amount exactly at threshold',{amount:10000},35,'review'],
 ['amount above threshold',{amount:10000.01},35,'review'],
 ['risk below threshold',{risk_score:79.99},0,'approve'],
 ['risk exactly at threshold',{risk_score:80},40,'review'],
 ['new account amount below threshold',{customer_age_days:6,amount:999.99},0,'approve'],
 ['new account amount at threshold',{customer_age_days:6,amount:1000},25,'review'],
 ['account exactly seven days old',{customer_age_days:7,amount:1000},0,'approve'],
 ['minor foreign payment',{currency:'EUR',merchant_country:'GB'},10,'approve'],
 ['review exactly at threshold',{amount:5000,card_present:false},20,'review'],
 ['block exactly at threshold',{risk_score:80,currency:'EUR',merchant_country:'GB'},50,'block_and_review'],
 ['decline exactly at threshold',{amount:10000,customer_age_days:6,card_present:false},80,'decline'],
 ['maximum combined risk',{amount:25000,risk_score:100,customer_age_days:0,currency:'EUR',merchant_country:'GB',card_present:false},130,'decline'],
 ['zero amount',{amount:0},0,'approve'],
 ['domestic merchant with foreign currency',{currency:'EUR'},0,'approve'],
];
export function dataset(){
 const rows=named.map(([name,changes,score,outcome],i)=>{
  const f={...baseline,...changes},want=expected(f);
  if(want.score!==score||want.outcome!==outcome)throw Error('Hand-calculated oracle disagrees: '+name);
  return {object_id:'boundary-'+String(i+1).padStart(3,'0'),name,fields:f,expected:want};
 });
 let state=0x5eed0001;const pick=values=>{state^=state<<13;state^=state>>>17;state^=state<<5;return values[(state>>>0)%values.length];};
 for(let i=0;i<240;i++){
  const f={amount:pick([0,100,999.99,1000,4999.99,5000,9999.99,10000,25000]),currency:pick(['USD','EUR','GBP']),merchant_country:pick(['US','GB','NG']),risk_score:pick([0,20,79.99,80,100]),customer_age_days:pick([0,6,7,30,365]),card_present:pick([true,false])};
  rows.push({object_id:'synthetic-'+String(i+1).padStart(3,'0'),name:'Deterministic synthetic combination '+(i+1),fields:f,expected:expected(f)});
 }
 return {version:1,seed:'0x5eed0001',synthetic:true,description:'Functional rule acceptance data; supplied risk_score is an input feature, not a trained fraud model.',rules,thresholds,rows};
}
if(process.argv[1]===fileURLToPath(import.meta.url)){
 const target=new URL('../testdata/fraud-synthetic-transactions.json',import.meta.url);
 writeFileSync(target,JSON.stringify(dataset(),null,2)+'\n');
 console.log('Wrote 256 synthetic transactions with independently calculated scores, outcomes and rule hits.');
}
