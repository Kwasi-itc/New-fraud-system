// Apply the checked-in local installer setting without restarting either DB.
// Never apply this helper to an arbitrary name, endpoint, or production server.
import assert from 'node:assert/strict';
import {spawnSync} from 'node:child_process';
for(const name of ['fraud-mysql-dev-mysql-1','fraud-mysql-dev-mysql-reference-1']){
 const inspected=spawnSync('docker',['inspect',name],{encoding:'utf8',windowsHide:true});assert.equal(inspected.status,0,'Local MySQL container unavailable');
 const [container]=JSON.parse(inspected.stdout);
 assert.equal(container.Config.Image,'mysql:8.4.8');assert.equal(container.Config.Labels['com.docker.compose.project'],'fraud-mysql-dev');
 assert.ok(container.Config.Env.includes('MYSQL_DATABASE=fraud'));
 const script="read -r MYSQL_PWD < /run/secrets/mysql_root_password; export MYSQL_PWD; mysql --protocol=socket --socket=/var/run/mysqld/mysqld.sock -uroot -e 'SET PERSIST log_bin_trust_function_creators=ON'";
 const result=spawnSync('docker',['exec',name,'sh','-c',script],{encoding:'utf8',windowsHide:true});assert.equal(result.status,0,'Cannot configure local restricted-installer setting; private output omitted');
}
console.log('Local MySQL installer setting aligned with the development Compose profile.');
