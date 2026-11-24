import { existsSync, readFileSync, writeFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { randomBytes } from 'node:crypto';
import { provisionPlatformKeys } from './platform-keys.mjs';
const root = fileURLToPath(new URL('../../../', import.meta.url));
const dir = path.resolve(process.env.PLATFORM_AUTH_DIRECTORY || path.join(root,'.local/auth-provider'));
if (!existsSync(path.join(dir,'auth.env'))) throw new Error('Run setup-local-provider.mjs first; preserve existing provider identity material.');
const tokenEndpointURL = process.env.AUTH_TOKEN_ENDPOINT_URL || 'http://localhost:3000/oauth/token';
provisionPlatformKeys(dir,{tokenEndpointURL});
const invitations=path.join(dir,'invitations.env');
if (!existsSync(invitations)) writeFileSync(invitations,`AUTH_INVITATIONS_ENABLED=false\nAUTH_INVITATION_KEY=${randomBytes(32).toString('base64')}\nAUTH_APP_ORIGIN=http://localhost:3000\nAUTH_SMTP_ADDR=mailpit:1025\nAUTH_SMTP_FROM=access@example.test\nAUTH_SMTP_LOCAL_CAPTURE=true\n`,{mode:0o600,flag:'wx'});

const envPath=path.join(root,'.env');
let env=existsSync(envPath)?readFileSync(envPath,'utf8'):'';
const retired=['SERVICE_AUTH_MODE','SERVICE_AUTH_TOKEN','AUTH_PROXY_TOKEN','DATA_MODEL_SERVICE_TOKEN','CASE_SERVICE_AUTH_TOKEN','CASE_SERVICE_TENANT_IDS','CASE_USER_JWT_KEYS_FILE','CASE_USER_JWT_ISSUER','CASE_USER_JWT_AUDIENCE','WORKFLOW_AUTHORING_TOKEN','SCREENING_AUTHORING_TOKEN','SCREENING_INTAKE_TOKEN','SCREENING_CALLBACK_TOKEN','SCREENING_OPERATOR_TOKEN','USER_REQUIRE_SESSION','USER_JWT_KEYS_FILE','USER_JWT_ISSUER','USER_JWT_AUDIENCE','SERVICE_AUTH_TENANT_IDS'];
env=env.split(/\r?\n/).filter(line=>!retired.some(key=>line.startsWith(`${key}=`))).join('\n');
const relative=path.relative(root,dir);
for(const [key,value] of Object.entries({PLATFORM_AUTH_DIRECTORY:relative,PLATFORM_AUTH_KEYS_FILE:`${relative}/public-keys.json`,CASE_SESSION_COOKIE:'__Host-fraud_session'})){
 const pattern=new RegExp(`^${key}=.*$`,'m');env=pattern.test(env)?env.replace(pattern,`${key}=${value}`):`${env.trimEnd()}\n${key}=${value}\n`;
}
writeFileSync(envPath,env,{mode:0o600});
console.log('Per-service signing identities and trusted public key ring ready. Apply ordered migrations and rebuild all APIs, workers, and the BFF together. Existing provider credentials and private keys were preserved.');
