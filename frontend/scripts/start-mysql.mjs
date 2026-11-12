import {readFileSync} from 'node:fs';
import {spawn} from 'node:child_process';
import {localOrigin} from './mysql-origin.mjs';
import {httpTest} from './http-test.mjs';
const remote=httpTest();
if(process.env.BACKEND_PROFILE!=='mysql'||!process.env.APP_ORIGIN||process.env.LOCAL_MYSQL_NO_AUTH)throw new Error('Authenticated MySQL frontend configuration required');
if(!remote)localOrigin(process.env.APP_ORIGIN);
for(const name of ['NEXT_PUBLIC_INGESTION_SERVICE_TOKEN','NEXT_PUBLIC_DECISION_ENGINE_SERVICE_TOKEN'])if(process.env[name])throw new Error('Browser-visible service credentials are not supported');
const secrets={AUTH_PROXY_TOKEN:'frontend_auth_proxy',INGESTION_AUTH_TOKEN:'frontend_ingestion',DECISION_ENGINE_AUTH_TOKEN:'frontend_decision',SCREENING_AUTH_TOKEN:'frontend_screening',SCREENING_OPERATOR_TOKEN:'frontend_screening_operator'};
if(remote)secrets.DATA_MODEL_AUTH_TOKEN='frontend_data_model';
for(const [name,file]of Object.entries(secrets)){
 const value=readFileSync('/run/secrets/'+file,'utf8').trim();
 if(!/^[a-f0-9]{64}$/.test(value))throw new Error('Invalid mounted service credential');
 process.env[name]=value;
}
process.env.SCREENING_AUTHORING_TOKEN=process.env.DECISION_ENGINE_AUTH_TOKEN;
process.env.WORKFLOW_AUTHORING_TOKEN=process.env.DECISION_ENGINE_AUTH_TOKEN;
const child=spawn(process.execPath,['server.js'],{stdio:'inherit',env:{...process.env,PORT:'3000',HOSTNAME:'0.0.0.0'}});
for(const signal of ['SIGTERM','SIGINT'])process.on(signal,()=>child.kill(signal));
child.on('exit',(code,signal)=>process.exit(code??(signal?1:0)));
