import {createHash} from 'node:crypto';
import {existsSync,readFileSync,writeFileSync} from 'node:fs';
import {spawnSync} from 'node:child_process';
import {isIPv4} from 'node:net';

export function accountHost(subnet){
  const parts=String(subnet).split('/'),prefix=Number(parts[1]);
  if(parts.length!==2||!isIPv4(parts[0])||!Number.isInteger(prefix)||prefix<1||prefix>32)throw Error('One explicit Docker IPv4 subnet is required');
  const mask=(0xffffffff<<(32-prefix))>>>0;
  return parts[0]+'/'+[24,16,8,0].map(shift=>(mask>>>shift)&255).join('.');
}
export function localAccountHost(){
  const result=spawnSync('docker',['network','inspect','fraud-mysql-dev_default'],{encoding:'utf8',windowsHide:true});
  if(result.status!==0)throw Error('Start the shared MySQL Docker network first');
  const networks=JSON.parse(result.stdout),network=networks[0];
  if(networks.length!==1||network.Name!=='fraud-mysql-dev_default'||network.Labels?.['com.docker.compose.project']!=='fraud-mysql-dev')throw Error('Expected the local fraud MySQL project network');
  const subnets=(network.IPAM?.Config??[]).filter(value=>isIPv4(value.Subnet?.split('/')[0]??''));
  if(subnets.length!==1)throw Error('Exactly one shared Docker IPv4 subnet is required');
  return accountHost(subnets[0].Subnet);
}
export function networkConfig(previous,expected,allow=false){
  if(previous.Database!=='fraud'||expected.Database!=='fraud'||previous.Address!==expected.Address)throw Error('Existing runtime database/address differs; reconcile explicitly');
  if(previous.AccountHost===expected.AccountHost)return previous;
  if(!allow)throw Error('Docker subnet changed; run the service setup script to reconcile its runtime accounts');
  if(!/^\d+\.\d+\.\d+\.\d+\/\d+\.\d+\.\d+\.\d+$/.test(expected.AccountHost))throw Error('An explicit local IPv4 account host is required');
  return {...previous,AccountHost:expected.AccountHost};
}
export function retainNetworkConfig(file,expected,allow=false){
  if(!existsSync(file)){writeFileSync(file,JSON.stringify(expected,null,2)+'\n',{flag:'wx',mode:0o600});return;}
  const raw=readFileSync(file,'utf8'),previous=JSON.parse(raw),next=networkConfig(previous,expected,allow);
  if(next===previous)return;
  const backup=file+'.before-network-'+createHash('sha256').update(raw).digest('hex').slice(0,16)+'.json';
  if(!existsSync(backup))writeFileSync(backup,raw,{flag:'wx',mode:0o600});
  else if(readFileSync(backup,'utf8')!==raw)throw Error('Retained network backup differs');
  writeFileSync(file,JSON.stringify(next,null,2)+'\n',{mode:0o600});
  console.log('Reconciled local runtime subnet; credentials and application settings were preserved.');
}
