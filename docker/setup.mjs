import {parseArgs} from 'node:util';
import {resolve} from 'node:path';
import {pathToFileURL} from 'node:url';
import {statSync} from 'node:fs';
import {setupPlatform} from './mysql-dev/platform.mjs';

export function options(args, cwd = process.cwd()) {
  const {values} = parseArgs({args, options: {
    port: {type: 'string', default: '8088'}, install: {type: 'boolean'},
    'skip-build': {type: 'boolean'}, 'users-file': {type: 'string'},
    'create-missing-users': {type: 'boolean'}, help: {type: 'boolean', short: 'h'},
  }});
  if (!/^\d+$/.test(values.port) || Number(values.port) < 1024 || Number(values.port) > 65535) throw Error('--port must be between 1024 and 65535');
  if (values['create-missing-users'] && !values['users-file']) throw Error('--create-missing-users requires --users-file');
  const usersFile = values['users-file'] && resolve(cwd, values['users-file']);
  if (usersFile && !statSync(usersFile).isFile()) throw Error('--users-file must identify a file');
  return {port: Number(values.port), install: Boolean(values.install), skipBuild: Boolean(values['skip-build']), usersFile, createMissingUsers: Boolean(values['create-missing-users']), help: Boolean(values.help)};
}
if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  try {
    if (Number(process.versions.node.split('.')[0]) < 22) throw Error('Node.js 22 or newer is required');
    const selected = options(process.argv.slice(2));
    if (selected.help) console.log(`Usage: node docker/setup.mjs [options]
Works on macOS, Linux and Windows. Requires Node.js 22+ and Docker Desktop
with Linux containers and Docker Compose 2.24.4+.

  --port 8088             Shared frontend/API/Keycloak port
  --install               Run owner migrations and rebuild backend images
  --skip-build            Reuse the existing frontend image
  --users-file PATH       Apply an explicit portable user manifest
  --create-missing-users  Allow creating users named by that manifest
  --help                  Show this help

Existing databases, memberships and credentials are retained.`);
    else await setupPlatform(selected);
  } catch (error) {
    console.error(error.message);
    console.error('Setup stopped. Correct the error and rerun node docker/setup.mjs; existing data and credentials are retained.');
    process.exitCode = 1;
  }
}
