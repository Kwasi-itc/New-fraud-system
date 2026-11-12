import assert from 'node:assert/strict';
import test from 'node:test';
import {applyUsers,dataModelBase,planUsers,providerClient,selectTenant,validateManifest} from './setup-mysql-users.mjs';

test('local data-model URL supports a gateway prefix without accepting arbitrary destinations',()=>{
  assert.equal(dataModelBase('http://localhost:8080'),'http://localhost:8080');
  assert.equal(dataModelBase('http://localhost:8088/services/data-model/'),'http://localhost:8088/services/data-model');
  for(const value of ['http://remote.example/services/data-model','http://localhost:8088/services/auth','http://localhost:8088/services/data-model?token=bad','http://user@localhost:8088/services/data-model'])assert.throws(()=>dataModelBase(value));
});

const tenant={id:'11111111-1111-4111-8111-111111111111',name:'Example tenant',external_key:'example',status:'active'};
const account={id:'22222222-2222-4222-8222-222222222222',username:'operator',enabled:true};
const manifest=()=>({version:1,tenant:{name:tenant.name},users:[{username:'operator',roles:['platform_viewer']}]});
const services=()=>({tenant:async()=>tenant,user:async()=>account});

test('manifest requires explicit known roles and rejects inline passwords',()=>{
  validateManifest(manifest());
  for(const roles of [[],['admin'],['platform_viewer','platform_viewer']]){
    const value=manifest();value.users[0].roles=roles;assert.throws(()=>validateManifest(value),/roles/);
  }
  const secret=manifest();secret.users[0].password='DO-NOT-LOG-THIS';
  assert.throws(()=>validateManifest(secret),error=>!error.message.includes('DO-NOT-LOG-THIS')&&/unsupported/.test(error.message));
  const duplicate=manifest();duplicate.users.push({username:'Operator',roles:['platform_admin']});
  assert.throws(()=>validateManifest(duplicate),/Duplicate/);
});
test('tenant lookup rejects ambiguous names and inactive tenants',()=>{
  assert.equal(selectTenant({external_key:'example'},[tenant]).id,tenant.id);
  assert.throws(()=>selectTenant({name:tenant.name},[tenant,{...tenant,id:account.id}]),/exactly one/);
  assert.throws(()=>selectTenant({id:tenant.id},[{...tenant,status:'suspended'}]),/active/);
  const value=manifest();value.tenant={id:tenant.id,name:tenant.name};
  assert.throws(()=>validateManifest(value),/Select one/);
});
test('planning existing users never changes accounts or provisions memberships',async()=>{
  const deps={...services(),create:()=>assert.fail('created during preview'),provision:()=>assert.fail('provisioned during preview')};
  const plan=await planUsers(manifest(),deps);
  assert.equal(plan[0].account.id,account.id);
  assert.deepEqual(plan[0].spec.roles,['platform_viewer']);
});
test('all accounts are preflighted before any membership changes',async()=>{
  const value=manifest();value.users.push({username:'suspended',roles:['platform_viewer']});
  let mutations=0;
  const deps={...services(),user:async username=>username==='operator'?account:{...account,username,enabled:false},provision:async()=>mutations++};
  await assert.rejects(()=>planUsers(value,deps),/disabled/);
  assert.equal(mutations,0);
});
test('creating a missing account requires explicit opt-in and a complete profile',async()=>{
  const deps={...services(),user:async()=>null};
  await assert.rejects(()=>planUsers(manifest(),deps),/--create-missing/);
  await assert.rejects(()=>planUsers(manifest(),deps,true),/first_name/);
  const value=manifest();Object.assign(value.users[0],{first_name:'Local',last_name:'Operator',email:'operator@example.test'});
  const plan=await planUsers(value,deps,true);assert.equal(plan[0].account,null);
});
test('replaying existing identities preserves passwords and uses explicit roles',async()=>{
  const calls=[];
  const deps={...services(),create:async()=>assert.fail('existing account password changed'),provision:async(spec,user,scope)=>calls.push([spec.roles,user.id,scope.id])};
  for(let i=0;i<2;i++)await applyUsers(await planUsers(manifest(),deps),deps);
  assert.deepEqual(calls,[[['platform_viewer'],account.id,tenant.id],[['platform_viewer'],account.id,tenant.id]]);
});
test('a failed membership stops the batch and does not create later users',async()=>{
  const value=manifest();value.users.push({username:'second',roles:['platform_viewer']});
  const deps={...services(),user:async username=>({...account,username}),provision:async()=>{throw Error('membership conflict');}};
  let calls=0;
  const provision=deps.provision;deps.provision=async(...args)=>{calls++;await provision(...args);};
  const plan=await planUsers(value,deps);
  await assert.rejects(()=>applyUsers(plan,deps),/membership conflict/);
  assert.equal(calls,1);
});
test('provider identity mismatches never reach auth provisioning',async()=>{
  const deps={...services(),user:async()=>({...account,username:'different-user'})};
  await assert.rejects(()=>planUsers(manifest(),deps),/not exact/);
  const value=manifest();Object.assign(value.users[0],{first_name:'Local',last_name:'Operator',email:'operator@example.test'});
  const plan=await planUsers(value,{...services(),user:async()=>null},true);
  await assert.rejects(()=>applyUsers(plan,{create:async()=>({...account,enabled:false}),provision:async()=>assert.fail('invalid provider account provisioned')}),/identity changed/);
});
test('provider tokens refresh after long provisioning without retrying account creation',async()=>{
  let clock=0,tokens=0,creates=0;
  const send=async(url,options)=>{
    if(url.endsWith('/token'))return Response.json({access_token:'fixture-token-'+(++tokens),expires_in:60});
    assert.equal(options.headers.Authorization,'Bearer fixture-token-'+tokens);
    if(options.method==='POST'){creates++;return new Response(null,{status:401});}
    return Response.json([]);
  };
  const client=providerClient('http://provider.test',{KC_BOOTSTRAP_ADMIN_USERNAME:'fixture',KC_BOOTSTRAP_ADMIN_PASSWORD:'fixture'},send,()=>clock);
  await client('http://provider.test/users');await client('http://provider.test/users');assert.equal(tokens,1);
  clock=45000;
  assert.equal((await client('http://provider.test/users',{method:'POST',body:'{}'})).status,401);
  assert.equal(tokens,2);assert.equal(creates,1);
});
