import {existsSync} from 'node:fs';
import {resolve} from 'node:path';
import {fileURLToPath} from 'node:url';
import {spawnSync} from 'node:child_process';
import {prepareFoundation} from './setup-files.mjs';
const root = fileURLToPath(new URL('../../', import.meta.url));
let existing = false;
if (!existsSync(resolve(root, '.local/mysql-dev/root-password.txt')) || !existsSync(resolve(root, '.local/mysql-dev/data-model-password.txt'))) {
  const result = spawnSync('docker', ['volume', 'ls', '--filter', 'name=^fraud-mysql-dev_mysql_dev_data$', '--format', '{{.Name}}'], {encoding: 'utf8', windowsHide: true});
  if (result.status !== 0) throw Error('Cannot inspect MySQL volumes; start Docker Desktop before setup.');
  existing = Boolean(result.stdout.trim());
}
prepareFoundation(root, existing);
console.log('Local MySQL configuration ready; existing credentials and data retained.');
