// Updates only the existing local fraud client and auth callback. Provider
// credentials and application signing keys remain in ignored local files.
import {readFileSync,writeFileSync} from 'node:fs';
import {fileURLToPath} from 'node:url';
import {resolve} from 'node:path';
import {frontendOrigin} from './mysql-origin.mjs';
import {issuerURLs} from '../../docker/keycloak/urls.mjs';
const root=fileURLToPath(new URL('../../',import.meta.url)),local=resolve(root,'.local/mysql-dev');
const origin=frontendOrigin(), callback=origin+'/api/auth/callback';
const configPath=resolve(local,'auth-runtime/config.json'),config=JSON.parse(readFileSync(configPath,'utf8'));
const issuer=issuerURLs(config.Provider.IssuerURL);
if(config.Provider.ClientID!=='fraud-auth')throw Error('Expected the existing local fraud Keycloak client');
const env=Object.fromEntries(readFileSync(resolve(root,'.local/auth-provider/keycloak.env'),'utf8').split(/\r?\n/).filter(line=>line&&!line.startsWith('#')).map(line=>{const at=line.indexOf('=');return [line.slice(0,at),line.slice(at+1)];}));
const request=(url,init)=>fetch(url,{...init,redirect:'error',signal:AbortSignal.timeout(15000)});
const token=await request(issuer.masterToken,{method:'POST',headers:{'Content-Type':'application/x-www-form-urlencoded'},body:new URLSearchParams({grant_type:'password',client_id:'admin-cli',username:env.KC_BOOTSTRAP_ADMIN_USERNAME,password:env.KC_BOOTSTRAP_ADMIN_PASSWORD})});
if(!token.ok)throw Error('Existing local Keycloak administrator authentication failed');
const headers={Authorization:'Bearer '+(await token.json()).access_token,'Content-Type':'application/json'};
const base=issuer.clients;const found=await request(base+'?clientId=fraud-auth',{headers});
if(!found.ok)throw Error('Cannot locate local fraud client');const clients=await found.json();if(clients.length!==1)throw Error('Expected exactly one local fraud client');
const client=clients[0];
if(!(client.redirectUris??[]).includes(callback)){
 const changed=await request(base+'/'+client.id,{method:'PUT',headers,body:JSON.stringify({...client,redirectUris:[...(client.redirectUris??[]),callback]})});
 if(!changed.ok)throw Error('Cannot register the frontend callback');
}
config.Provider.RedirectURL=callback;config.InvitationOrigin=origin;
if(process.env.FRAUD_UNIFIED_PLATFORM==='1')config.Provider.BackchannelOrigin='http://keycloak:8080';
writeFileSync(configPath,JSON.stringify(config,null,2)+'\n',{mode:0o600});
console.log('Configured the existing fraud login callback for '+origin+'.');
