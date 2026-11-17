// Independent provider development setup. It never reads the fraud .env/DB,
// changes identity-provider configuration, or activates official sources.
import { randomBytes } from 'node:crypto';
import { existsSync, lstatSync, mkdirSync, readFileSync, realpathSync, writeFileSync } from 'node:fs';
import path from 'node:path';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';

const root=fileURLToPath(new URL('..',import.meta.url));
const args=process.argv.slice(2);let port=8091;
for(let i=0;i<args.length;i++){if(args[i]==='--port'){port=Number(args[++i]);if(!Number.isInteger(port)||port<1024||port>65535)throw Error('Choose a port from 1024 to 65535');}else if(!['--prepare-only','--skip-build'].includes(args[i]))throw Error('Unknown provider setup argument');}
const uid=process.getuid?.()??65532,gid=process.getgid?.()??65532;
if(uid===0)throw Error('Run local setup as a non-root user so protected secret files and the non-root container share file ownership.');
const directory=path.join(root,'.local','provider-dev');mkdirSync(directory,{recursive:true,mode:0o700});
const resolved=realpathSync(directory),workspace=realpathSync(root);
if(!resolved.toLowerCase().startsWith((workspace+path.sep).toLowerCase()))throw Error('Provider secret directory must remain inside this workspace');
function password(file){const p=path.join(directory,file);if(!existsSync(p))writeFileSync(p,randomBytes(32).toString('hex')+'\n',{mode:0o600,flag:'wx'});if(!lstatSync(p).isFile()||lstatSync(p).isSymbolicLink())throw Error('Provider secret must be a regular file');const value=readFileSync(p,'utf8').trim();if(!/^[a-f0-9]{64}$/.test(value))throw Error('Existing provider secret has an unexpected format; it was not replaced');return value;}
const owner=password('owner-password.txt'),reader=password('reader-password.txt'),collector=password('collector-password.txt');
function secret(file,value){const p=path.join(directory,file);if(existsSync(p)){if(!lstatSync(p).isFile()||lstatSync(p).isSymbolicLink()||readFileSync(p,'utf8').trim()!==value)throw Error('Existing provider connection secret differs; no credentials were replaced');return;}writeFileSync(p,value+'\n',{mode:0o600,flag:'wx'});}
secret('owner-dsn.txt',`postgres://provider_owner:${owner}@provider-db:5432/screening_provider?sslmode=disable`);
secret('reader-dsn.txt',`postgres://provider_runtime:${reader}@provider-db:5432/screening_provider?sslmode=disable`);
secret('collector-dsn.txt',`postgres://provider_collector:${collector}@provider-db:5432/screening_provider?sslmode=disable`);
const config={PROVIDER_SECRET_DIR:directory.replaceAll('\\','/'),PROVIDER_PORT:String(port),PROVIDER_RUN_UID:String(uid),PROVIDER_RUN_GID:String(gid)};
const quote=value=>`'${value.replaceAll("'","\\'")}'`;const envFile=path.join(directory,'compose.env');
if(existsSync(envFile)&&(!lstatSync(envFile).isFile()||lstatSync(envFile).isSymbolicLink()))throw Error('Provider Compose config must be a regular file');
writeFileSync(envFile,Object.entries(config).map(([key,value])=>`${key}=${quote(value)}`).join('\n')+'\n',{mode:0o600});
const base=['compose','--env-file',envFile,'-p','screening-provider-dev','-f','docker-compose.provider.yml'];
function docker(extra){const result=spawnSync('docker',[...base,...extra],{cwd:root,stdio:'inherit',shell:false});if(result.error||result.status!==0)throw Error('Provider Docker command failed; inspect the preceding output');}
docker(['config','--quiet']);
if(!args.includes('--prepare-only'))docker(['up',...(args.includes('--skip-build')?[]:['--build']),'-d','--wait']);
console.log(args.includes('--prepare-only')?'Provider secrets/configuration prepared; no containers started.':`Provider is ready at http://localhost:${port}/docs`);
console.log('Catalog starts empty. No official source, client credential, tenant grant or production activation was created.');
