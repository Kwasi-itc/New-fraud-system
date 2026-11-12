// Local operator bootstrap: provider identities plus audited MySQL memberships.
// No password resets, role overwrites, account reactivation or direct SQL writes.
import {createHash,randomBytes,randomUUID} from 'node:crypto';
import {mkdirSync,readFileSync,statSync,writeFileSync} from 'node:fs';
import {spawnSync} from 'node:child_process';
import {fileURLToPath,pathToFileURL} from 'node:url';
import {resolve} from 'node:path';
import {parseArgs} from 'node:util';
import {issuerURLs} from '../../../docker/keycloak/urls.mjs';

const root=fileURLToPath(new URL('../../../',import.meta.url));
export const supportedRoles=['tenant_admin','case_manager_admin','case_investigator','platform_viewer','platform_builder','platform_publisher','platform_admin'];
const uuid=/^(?!00000000-0000-0000-0000-000000000000$)[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/i;
function requireValue(ok,message){if(!ok)throw Error(message);}
function object(value,allowed,label){
  requireValue(value&&typeof value==='object'&&!Array.isArray(value),label+' must be an object');
  requireValue(Object.keys(value).every(key=>allowed.includes(key)),label+' contains unsupported fields');
}
function text(value,max){return typeof value==='string'&&value.trim()===value&&value.length>0&&Buffer.byteLength(value)<=max&&!/[\x00-\x1f\x7f]/.test(value);}
export function validateManifest(value){
  object(value,['version','tenant','users'],'Manifest');
  requireValue(value.version===1,'Manifest version must be 1');
  object(value.tenant,['id','name','external_key'],'Tenant selector');
  const keys=Object.keys(value.tenant);
  requireValue(keys.length===1&&text(value.tenant[keys[0]],255),'Select one tenant by id, name or external_key');
  if(keys[0]==='id')requireValue(uuid.test(value.tenant.id),'Tenant id must be a nonzero UUID');
  requireValue(Array.isArray(value.users)&&value.users.length>0&&value.users.length<=50,'Specify between 1 and 50 users');
  const seen=new Set();
  for(const user of value.users){
    object(user,['username','name','first_name','last_name','email','roles'],'User');
    requireValue(typeof user.username==='string'&&/^[A-Za-z0-9][A-Za-z0-9._@+-]{0,63}$/.test(user.username),'Invalid username');
    const key=user.username.toLowerCase();requireValue(!seen.has(key),'Duplicate username');seen.add(key);
    for(const key of ['name','first_name','last_name'])if(user[key]!==undefined)requireValue(text(user[key],200),'Invalid user display/profile name');
    if(user.email!==undefined)requireValue(text(user.email,254)&&/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(user.email),'Invalid user email');
    requireValue(Array.isArray(user.roles)&&user.roles.length>0&&new Set(user.roles).size===user.roles.length&&user.roles.every(role=>supportedRoles.includes(role)),'Explicit supported roles are required');
  }
  return value;
}
export function selectTenant(selector,tenants){
  const key=Object.keys(selector)[0];
  const matches=tenants.filter(tenant=>tenant[key]===selector[key]);
  requireValue(matches.length===1,'Tenant selector must match exactly one registered tenant; use its id if names are ambiguous');
  requireValue(uuid.test(matches[0].id)&&matches[0].status==='active','The selected tenant must be active');
  return matches[0];
}

// Resolve and validate the complete plan before any account/membership mutation.
export async function planUsers(manifest,services,createMissing=false){
  validateManifest(manifest);
  const tenant=await services.tenant(manifest.tenant),plan=[];
  for(const spec of manifest.users){
    const account=await services.user(spec.username);
    if(account){
      requireValue(uuid.test(account.id)&&account.username.toLowerCase()===spec.username.toLowerCase(),'Provider identity lookup was not exact');
      requireValue(account.enabled===true,'An existing provider account is disabled; bootstrap will not reactivate it');
    }else{
      requireValue(createMissing,'A provider account is missing; create it in Keycloak or explicitly use --create-missing');
      requireValue(spec.first_name&&spec.last_name&&spec.email,'New accounts need first_name, last_name and email in the manifest');
    }
    plan.push({spec,account,tenant});
  }
  return plan;
}
export async function applyUsers(plan,services){
  for(const item of plan){
    const account=item.account??await services.create(item.spec);
    requireValue(uuid.test(account.id)&&account.username.toLowerCase()===item.spec.username.toLowerCase()&&account.enabled===true,'Provider identity changed during setup');
    await services.provision(item.spec,account,item.tenant);
  }
}
function readJSON(path,max=65536){
  requireValue(statSync(path).size<=max,'Configuration file is too large');
  try{return JSON.parse(readFileSync(path,'utf8'));}catch{throw Error('Configuration must contain valid JSON; values omitted');}
}
function readEnv(path){return Object.fromEntries(readFileSync(path,'utf8').split(/\r?\n/).filter(line=>line&&!line.startsWith('#')).map(line=>{const i=line.indexOf('=');return [line.slice(0,i),line.slice(i+1)];}));}
function localURL(value){const url=new URL(value);requireValue(url.protocol==='http:'&&['localhost','127.0.0.1','[::1]'].includes(url.hostname)&&!url.username&&!url.password&&!url.search&&!url.hash,'Only local development service URLs are supported');return url;}
export function dataModelBase(value){
  const url=localURL(value);
  requireValue(['/', '/services/data-model', '/services/data-model/'].includes(url.pathname),'Unexpected local data-model base URL');
  return url.origin+(url.pathname==='/'?'':url.pathname.replace(/\/$/,''));
}
async function request(url,options={}){
  try{return await fetch(url,{...options,redirect:'error',signal:AbortSignal.timeout(20000)});}
  catch{throw Error('Local service request failed; no automatic retry was attempted');}
}
async function json(response,label){
  requireValue(response.ok,label+' failed (HTTP '+response.status+')');
  const reader=response.body.getReader(),chunks=[];let size=0;
  for(;;){const {done,value}=await reader.read();if(done)break;size+=value.byteLength;if(size>2<<20){await reader.cancel();throw Error(label+' response exceeded its limit');}chunks.push(value);}
  try{return JSON.parse(Buffer.concat(chunks).toString('utf8'));}catch{throw Error(label+' returned invalid JSON; response omitted');}
}
export function providerClient(origin,admin,send=request,now=Date.now){
  let accessToken,refreshAt=0;
  return async(url,options={})=>{
    if(!accessToken||now()>=refreshAt){
      const issuedAt=now();
      const token=await json(await send(origin+'/realms/master/protocol/openid-connect/token',{method:'POST',headers:{'Content-Type':'application/x-www-form-urlencoded'},body:new URLSearchParams({grant_type:'password',client_id:'admin-cli',username:admin.KC_BOOTSTRAP_ADMIN_USERNAME,password:admin.KC_BOOTSTRAP_ADMIN_PASSWORD})}),'Provider administrator login');
      requireValue(typeof token.access_token==='string'&&token.access_token.length>0&&Number.isFinite(token.expires_in)&&token.expires_in>0,'Provider administrator token is invalid');
      accessToken=token.access_token;
      refreshAt=issuedAt+Math.max(0,token.expires_in-30)*1000;
    }
    // Refresh before a request; an ambiguous account-creation POST is never retried.
    return send(url,{...options,headers:{'Content-Type':'application/json',Authorization:'Bearer '+accessToken}});
  };
}
async function main(){
  const {values}=parseArgs({options:{file:{type:'string'},apply:{type:'boolean',default:false},'create-missing':{type:'boolean',default:false},'data-model-url':{type:'string',default:'http://localhost:8080'},help:{type:'boolean',default:false}},strict:true});
  if(values.help){console.log('node backend/auth-service/scripts/setup-mysql-users.mjs --file <manifest.json> [--create-missing] [--apply]\nPreview is the default. --apply provisions the explicit roles. --create-missing creates local Keycloak users with unique temporary passwords.');return;}
  requireValue(values.file,'Provide --file <manifest.json>; see mysql-users.example.json');
  const manifest=validateManifest(readJSON(resolve(values.file)));
  const local=resolve(root,'.local/mysql-dev'),runtime=resolve(local,'auth-runtime');
  const config=readJSON(resolve(runtime,'config.json'));
  requireValue(config.Database==='fraud'&&config.Provider.ClientID==='fraud-auth','Expected the existing local MySQL fraud auth profile');
  const issuer=issuerURLs(config.Provider.IssuerURL),dataModel=dataModelBase(values['data-model-url']);
  const admin=readEnv(resolve(root,'.local/auth-provider/keycloak.env'));
  requireValue(admin.KC_BOOTSTRAP_ADMIN_USERNAME&&admin.KC_BOOTSTRAP_ADMIN_PASSWORD,'Local provider administrator credentials are missing');
  const providerRequest=providerClient(issuer.base,admin);
  const providerUsers=issuer.users;
  const records=[];
  const privateDir=resolve(local,'user-bootstrap'),provisionDir=resolve(runtime,'user-bootstrap');
  const record=(event)=>{
    records.push(event);mkdirSync(privateDir,{recursive:true,mode:0o700});
    writeFileSync(resolve(privateDir,'last-run.json'),JSON.stringify({at:new Date().toISOString(),events:records},null,2)+'\n',{mode:0o600});
  };
  const services={
    async tenant(selector){
      const path=selector.id?'/v1/tenants/'+selector.id:'/v1/tenants';
      const result=await json(await request(dataModel+path),'Tenant lookup');
      const tenants=selector.id?[result.tenant]:result.tenants;
      requireValue(Array.isArray(tenants)&&tenants.length<=5000,'Tenant catalog is unavailable or too large; select an id');
      return selectTenant(selector,tenants);
    },
    async user(username){
      const matches=await json(await providerRequest(providerUsers+'?username='+encodeURIComponent(username)+'&exact=true&max=2'),'Provider user lookup');
      requireValue(Array.isArray(matches)&&matches.length<=1,'Provider username is ambiguous');
      return matches[0]??null;
    },
    async create(spec){
      const password=randomBytes(32).toString('base64url');
      const key=createHash('sha256').update(spec.username.toLowerCase()).digest('hex').slice(0,16)+'-'+randomUUID();
      mkdirSync(privateDir,{recursive:true,mode:0o700});
      const credentialsPath=resolve(privateDir,key+'.credentials.json');
      // Save before the POST so an uncertain response cannot lose the password.
      const receipt={username:spec.username,temporary_password:password,status:'creation_prepared'};
      const save=()=>writeFileSync(credentialsPath,JSON.stringify(receipt,null,2)+'\n',{mode:0o600});
      writeFileSync(credentialsPath,JSON.stringify(receipt,null,2)+'\n',{flag:'wx',mode:0o600});
      let response;
      try{response=await providerRequest(providerUsers,{method:'POST',body:JSON.stringify({username:spec.username,enabled:true,firstName:spec.first_name,lastName:spec.last_name,email:spec.email,emailVerified:false,credentials:[{type:'password',value:password,temporary:true}]})});}
      catch(error){receipt.status='creation_unconfirmed';save();record({username:spec.username,status:receipt.status,credentials_file:credentialsPath});throw error;}
      if(response.status!==201){receipt.status='creation_rejected';save();throw Error('Provider account creation rejected (HTTP '+response.status+'); existing accounts were not changed');}
      receipt.status='created';save();
      record({username:spec.username,status:'provider_created',credentials_file:credentialsPath});
      const account=await services.user(spec.username);
      requireValue(account&&uuid.test(account.id),'Created provider account could not be resolved; inspect the local receipt before retrying');
      const location=response.headers.get('location');
      requireValue(location&&new URL(location).pathname.endsWith('/users/'+account.id),'Created provider identity did not match its receipt');
      receipt.subject=account.id;save();
      console.log('Created '+spec.username+'; temporary password saved privately at '+credentialsPath);
      return account;
    },
    async provision(spec,account,tenant){
      mkdirSync(provisionDir,{recursive:true,mode:0o700});
      const name=createHash('sha256').update(tenant.id+'\0'+account.id).digest('hex')+'.json';
      const command={TenantID:tenant.id,Issuer:config.Provider.IssuerURL,Subject:account.id,Name:spec.name||[account.firstName,account.lastName].filter(Boolean).join(' ')||account.username,Roles:spec.roles};
      writeFileSync(resolve(provisionDir,name),JSON.stringify(command,null,2)+'\n',{mode:0o600});
      const child=spawnSync('docker',['compose','--profile','install','--env-file',resolve(local,'compose.env'),'-f','docker-compose-mysql.yml','run','--rm','--no-deps','migrate-auth','/app/install-auth','--provision','/runtime/user-bootstrap/'+name],{cwd:resolve(root,'backend/auth-service'),encoding:'utf8',windowsHide:true,timeout:240000,maxBuffer:4<<20});
      if(child.status!==0){record({username:spec.username,subject:account.id,tenant_id:tenant.id,status:'membership_unconfirmed'});throw Error('Auth provisioning did not confirm success for '+spec.username+'. Review membership conflicts or suspension through auth administration; bootstrap does not overwrite them. The same manifest can be rerun to confirm an uncertain result.');}
      record({username:spec.username,subject:account.id,tenant_id:tenant.id,roles:spec.roles,status:'membership_confirmed'});
      console.log('Provisioned '+spec.username+' in '+tenant.name+': '+spec.roles.join(', '));
    },
  };
  const plan=await planUsers(manifest,services,values['create-missing']);
  const loginTenant=config.DefaultTenantID;
  requireValue(!loginTenant||loginTenant==='00000000-0000-0000-0000-000000000000'||loginTenant===plan[0].tenant.id,'The configured login tenant differs from this manifest; reconcile auth configuration explicitly');
  console.log(JSON.stringify({mode:values.apply?'apply':'preview',users:plan.map(({spec,account,tenant})=>({username:spec.username,provider_action:account?'reuse':'create',subject:account?.id,tenant:tenant.name,tenant_id:tenant.id,roles:spec.roles}))},null,2));
  if(values.apply)await applyUsers(plan,services);
  else console.log('Preview only: no accounts or memberships changed. Add --apply to provision.');
}
if(process.argv[1]&&pathToFileURL(resolve(process.argv[1])).href===import.meta.url){
  main().catch(error=>{console.error('User setup failed: '+error.message);process.exitCode=1;});
}
