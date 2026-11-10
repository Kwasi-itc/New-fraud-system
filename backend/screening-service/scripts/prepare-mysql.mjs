import {randomBytes} from 'node:crypto';
import {existsSync,mkdirSync,readFileSync,writeFileSync,copyFileSync} from 'node:fs';
import {fileURLToPath} from 'node:url';
import {resolve} from 'node:path';
const root=fileURLToPath(new URL('../../../',import.meta.url)),local=resolve(root,'.local/mysql-dev'),runtime=resolve(local,'screening-runtime');
const auth=JSON.parse(readFileSync(resolve(local,'auth-runtime/config.json'),'utf8'));
if(auth.Database!=='fraud'||!auth.AccountHost||!auth.Issuer||!auth.Audience||!auth.KeyID)throw new Error('Complete the existing shared auth MySQL setup first.');
for(const name of ['case-service-token.txt','ingestion-runtime-token.txt'])if(!/^[a-f0-9]{64}$/.test(readFileSync(resolve(local,name),'utf8')))throw new Error('Complete the case/ingestion MySQL setup first.');
mkdirSync(runtime,{recursive:true});
for(const name of ['screening-api-password.txt','screening-worker-password.txt','screening-service-token.txt','screening-operator-token.txt','screening-intake-token.txt']){const path=resolve(local,name);if(!existsSync(path))writeFileSync(path,randomBytes(32).toString('hex'),{flag:'wx',mode:0o600});if(!/^[a-f0-9]{64}$/.test(readFileSync(path,'utf8')))throw new Error('Invalid retained screening credential file: '+name);}
copyFileSync(resolve(local,'auth-public.pem'),resolve(runtime,'auth-public.pem'));
const c={Address:'mysql:3306',Database:'fraud',AccountHost:auth.AccountHost,Issuer:auth.Issuer,Audience:auth.Audience,KeyID:auth.KeyID,Origin:'http://localhost:8085',CaseURL:'http://case-manager-service:8086',IngestionURL:'http://ingestion-service:8081'};
const path=resolve(runtime,'config.json');if(!existsSync(path))writeFileSync(path,JSON.stringify(c,null,2)+'\n',{flag:'wx',mode:0o600});else{const old=JSON.parse(readFileSync(path,'utf8'));if(old.Database!==c.Database||old.AccountHost!==c.AccountHost||old.Issuer!==c.Issuer)throw new Error('Existing screening database/network/issuer differs; reconcile explicitly.');}
console.log('Prepared private screening MySQL configuration; existing credentials retained and auth public key reused.');
