// Private local setup only. Existing credentials and signing keys are preserved.
import {randomBytes,generateKeyPairSync,createPublicKey} from 'node:crypto';
import {existsSync,mkdirSync,readFileSync,writeFileSync} from 'node:fs';
import {fileURLToPath} from 'node:url';
import {resolve} from 'node:path';
import {localAccountHost,retainNetworkConfig} from '../../../docker/mysql-dev/runtime-network.mjs';

const root=fileURLToPath(new URL('../../../',import.meta.url));
const local=resolve(root,'.local/mysql-dev'),runtime=resolve(local,'auth-runtime');mkdirSync(runtime,{recursive:true});
const providerFile=resolve(root,'.local/auth-provider/auth.env');
if(!existsSync(providerFile))throw new Error('Run scripts/setup-local-provider.mjs first; provider credentials are required.');
const env=Object.fromEntries(readFileSync(providerFile,'utf8').split(/\r?\n/).filter(line=>line&&!line.startsWith('#')).map(line=>{const at=line.indexOf('=');return [line.slice(0,at),line.slice(at+1)];}));
function retain(name,value,pattern){const path=resolve(local,name);if(!existsSync(path))writeFileSync(path,value,{flag:'wx',mode:0o600});const saved=readFileSync(path,'utf8');if(!pattern.test(saved))throw new Error('Invalid existing local secret: '+name);return saved;}
for(const role of ['api','worker'])retain('auth-'+role+'-password.txt',randomBytes(32).toString('hex'),/^[a-f0-9]{64}$/);
retain('auth-proxy-token.txt',randomBytes(32).toString('hex'),/^[a-f0-9]{64}$/);
retain('auth-invitation-key.txt',randomBytes(32).toString('base64'),/^[A-Za-z0-9+/]{43}=$/);
const savedClient=retain('auth-oidc-client-secret.txt',env.OIDC_CLIENT_SECRET??'',/^[^\r\n]{1,4096}$/);
if(savedClient!==env.OIDC_CLIENT_SECRET)throw new Error('Provider client secret changed; reconcile explicitly rather than rotating silently.');
const privateFile=resolve(local,'auth-private.pem'),publicFile=resolve(local,'auth-public.pem');
if(existsSync(privateFile)!==existsSync(publicFile))throw new Error('Restore the matching signing key pair; incomplete key material will not be replaced.');
if(!existsSync(privateFile)){const pair=generateKeyPairSync('rsa',{modulusLength:2048,publicKeyEncoding:{type:'spki',format:'pem'},privateKeyEncoding:{type:'pkcs8',format:'pem'}});writeFileSync(privateFile,pair.privateKey,{flag:'wx',mode:0o600});writeFileSync(publicFile,pair.publicKey,{flag:'wx',mode:0o600});}
if(createPublicKey(readFileSync(privateFile)).export({type:'spki',format:'pem'})!==readFileSync(publicFile,'utf8'))throw new Error('Signing key pair mismatch.');
const host=localAccountHost();
const provider={Environment:env.AUTH_ENV,IssuerURL:env.OIDC_ISSUER_URL,BackchannelOrigin:env.OIDC_BACKCHANNEL_ORIGIN,ClientID:env.OIDC_CLIENT_ID,ClientSecret:'',RedirectURL:env.OIDC_REDIRECT_URL};
const issuerURL=new URL(provider.IssuerURL);if(issuerURL.pathname==='/identity/realms/fraud')provider.BackchannelOrigin='http://keycloak:8080';else if(['localhost','127.0.0.1'].includes(issuerURL.hostname))provider.BackchannelOrigin='http://host.docker.internal:'+issuerURL.port;
const config={Address:'mysql:3306',Database:'fraud',AppOrigin:'http://localhost:8087',InvitationOrigin:'http://localhost:3000',AccountHost:host,Issuer:'http://localhost:8087',Audience:'fraud-platform',KeyID:'local-auth-mysql-v1',DefaultTenantID:'00000000-0000-0000-0000-000000000000',Provider:provider,SMTP:{Address:'auth-mail:1025',From:'auth@local.invalid',LocalCapture:true}};
retainNetworkConfig(resolve(runtime,'config.json'),config,process.argv.includes('--reconcile-network'));
console.log('Auth MySQL private configuration prepared; existing secrets and keys preserved.');
