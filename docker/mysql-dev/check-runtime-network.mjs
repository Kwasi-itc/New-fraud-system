// A changed Docker subnet requires the owner installers to reconcile restricted
// runtime accounts before starting services. Print only a boolean, no secrets.
import {readFileSync, existsSync} from 'node:fs';
import {fileURLToPath} from 'node:url';
import {localAccountHost} from './runtime-network.mjs';
const expected = localAccountHost();
const aligned = ['auth', 'case', 'screening'].every(owner => {
  const path = fileURLToPath(new URL(`../../.local/mysql-dev/${owner}-runtime/config.json`, import.meta.url));
  return existsSync(path) && JSON.parse(readFileSync(path, 'utf8')).AccountHost === expected;
});
console.log(aligned ? 'true' : 'false');
