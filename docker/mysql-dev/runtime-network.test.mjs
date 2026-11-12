import assert from 'node:assert/strict';
import test from 'node:test';
import {mkdtempSync,readdirSync,readFileSync,writeFileSync,unlinkSync,rmdirSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {accountHost,networkConfig,retainNetworkConfig} from './runtime-network.mjs';
const old={Database:'fraud',Address:'mysql:3306',AccountHost:'172.24.0.0/255.255.0.0',Provider:{RedirectURL:'http://localhost:3002/api/auth/callback'},DefaultTenantID:'retained',ServiceTenants:['existing'],ServiceTenantMode:'allowlist'};
const expected={...old,AccountHost:'172.18.0.0/255.255.0.0'};
test('network masks are explicit IPv4 ranges',()=>{
 assert.equal(accountHost('172.18.0.0/16'),expected.AccountHost);
 assert.equal(accountHost('127.0.0.1/32'),'127.0.0.1/255.255.255.255');
 for(const bad of ['0.0.0.0/0','::/64','172.18.0.0/33','172.18.0.0','999.0.0.0/16'])assert.throws(()=>accountHost(bad));
});
test('reconciliation is explicit and cannot change the database or address',()=>{
 assert.throws(()=>networkConfig(old,expected),/subnet changed/);
 assert.throws(()=>networkConfig({...old,Database:'other'},expected,true),/database\/address/);
 assert.throws(()=>networkConfig({...old,Address:'remote:3306'},expected,true),/database\/address/);
 assert.equal(networkConfig(old,old),old);
});
test('only the account host changes; callbacks and tenant access policies survive',()=>{
 assert.deepEqual(networkConfig(old,{...expected,Provider:{},ServiceTenants:[]},true),{...old,AccountHost:expected.AccountHost});
});
test('a changed network has one immutable backup and repeats preserve settings',()=>{
 const directory=mkdtempSync(join(tmpdir(),'fraud-network-'));
 try{
  const file=join(directory,'config.json'),raw=JSON.stringify(old)+'\n';writeFileSync(file,raw);
  retainNetworkConfig(file,expected,true);
  const backup=readdirSync(directory).find(name=>name.includes('.before-network-'));assert.ok(backup);
  assert.equal(readFileSync(join(directory,backup),'utf8'),raw);
  const updated=readFileSync(file,'utf8');retainNetworkConfig(file,expected,true);
  assert.equal(readFileSync(file,'utf8'),updated);assert.equal(readdirSync(directory).length,2);
 }finally{for(const file of readdirSync(directory))unlinkSync(join(directory,file));rmdirSync(directory);}
});
