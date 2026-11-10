// Local preparation preserves credentials and reuses the auth public key only.
import {randomBytes} from 'node:crypto';
import {existsSync,mkdirSync,readFileSync,writeFileSync,copyFileSync} from 'node:fs';
import {fileURLToPath} from 'node:url';
import {resolve} from 'node:path';
const root=fileURLToPath(new URL('../../../',import.meta.url));
const local=resolve(root,'.local/mysql-dev'),runtime=resolve(local,'case-runtime');
const auth=JSON.parse(readFileSync(resolve(local,'auth-runtime/config.json'),'utf8'));
if(auth.Database!=='fraud'||!auth.AccountHost||!auth.Issuer||!auth.Audience||!auth.KeyID)throw new Error('Complete the shared MySQL auth setup first.');
mkdirSync(runtime,{recursive:true});
for(const name of ['case-api-password.txt','case-worker-password.txt','case-service-token.txt','case-publisher-token.txt']){
 const file=resolve(local,name);if(!existsSync(file))writeFileSync(file,randomBytes(32).toString('hex'),{flag:'wx',mode:0o600});
 if(!/^[a-f0-9]{64}$/.test(readFileSync(file,'utf8')))throw new Error('Invalid existing local case credential: '+name);
}
copyFileSync(resolve(local,'auth-public.pem'),resolve(runtime,'auth-public.pem'));
const config={Address:'mysql:3306',Database:'fraud',AccountHost:auth.AccountHost,Issuer:auth.Issuer,Audience:auth.Audience,KeyID:auth.KeyID,ServiceTenants:[],ServiceTenantMode:'registered',PublisherURL:''};
const file=resolve(runtime,'config.json');
if(!existsSync(file))writeFileSync(file,JSON.stringify(config,null,2)+'\n',{flag:'wx',mode:0o600});
else{const old=JSON.parse(readFileSync(file,'utf8'));if(old.Database!=='fraud'||old.AccountHost!==auth.AccountHost)throw new Error('Existing case database/network differs; reconcile explicitly.');if(!old.ServiceTenantMode){old.ServiceTenantMode=old.ServiceTenants?.length?'allowlist':'registered';writeFileSync(file,JSON.stringify(old,null,2)+'\n',{mode:0o600});}}
console.log('Case MySQL configuration prepared; auth public key reused and existing credentials retained.');
