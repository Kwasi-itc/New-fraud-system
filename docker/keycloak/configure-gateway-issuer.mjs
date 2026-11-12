// Coordinate the public issuer with auth-owned identity keys. Existing user IDs,
// memberships, session rows, invitation tokens and credentials stay intact.
import {existsSync, mkdirSync, readFileSync, writeFileSync} from 'node:fs';
import {spawnSync} from 'node:child_process';
import {createHash} from 'node:crypto';
import {fileURLToPath} from 'node:url';
import {resolve} from 'node:path';
import {gatewayIssuer, issuerURLs} from './urls.mjs';
const root = fileURLToPath(new URL('../../', import.meta.url));
const local = resolve(root, '.local/mysql-dev');
const directory = resolve(local, 'issuer-move');
const configFile = resolve(local, 'auth-runtime/config.json');
const envFile = resolve(root, '.local/auth-provider/auth.env');
const target = gatewayIssuer().issuer;
mkdirSync(directory, {recursive: true, mode: 0o700});
const rawEnv = readFileSync(envFile, 'utf8');
const env = Object.fromEntries(rawEnv.split(/\r?\n/).filter(line => line && !line.startsWith('#')).map(line => {const at = line.indexOf('='); return [line.slice(0, at), line.slice(at + 1)];}));
const config = existsSync(configFile) ? JSON.parse(readFileSync(configFile, 'utf8')) : null;
const from = config?.Provider.IssuerURL ?? env.OIDC_ISSUER_URL;
issuerURLs(from);
function backup(file, label) {
  const raw = readFileSync(file);
  const hash = createHash('sha256').update(raw).digest('hex');
  const saved = resolve(directory, label + '-' + hash + '.backup');
  if (!existsSync(saved)) writeFileSync(saved, raw, {flag: 'wx', mode: 0o600});
  else if (!readFileSync(saved).equals(raw)) throw Error('Retained issuer configuration backup differs');
}
function docker(args) {
  const result = spawnSync('docker', args, {cwd: root, stdio: 'inherit', windowsHide: true});
  if (result.status !== 0) throw Error('Issuer configuration stopped; retained private backups are in .local/mysql-dev/issuer-move/');
}
if (from !== target) {
  backup(envFile, 'auth-env');
  if (config) {
    backup(configFile, 'auth-runtime');
    const compose = ['compose', '--profile', 'install', '-f', resolve(root, 'backend/auth-service/docker-compose-mysql.yml')];
    // Build before stopping writers. The owner command changes keys in one DB
    // transaction and refuses identity collisions; it never reapplies grants.
    docker([...compose, 'build', 'migrate-auth']);
    docker([...compose, 'stop', 'auth-service', 'auth-worker']);
    docker([...compose, 'run', '--rm', '--no-deps', 'migrate-auth', '/app/mysql-issuer-move', '--from', from, '--to', target]);
  } else if (env.OIDC_ISSUER_URL !== target) {
    throw Error('Restore the retained auth runtime configuration before moving an existing issuer');
  }
}
let nextEnv = rawEnv;
for (const [key, value] of Object.entries({OIDC_ISSUER_URL: target, OIDC_BACKCHANNEL_ORIGIN: 'http://keycloak:8080'})) {
  const pattern = new RegExp('^' + key + '=.*$', 'm');
  nextEnv = pattern.test(nextEnv) ? nextEnv.replace(pattern, key + '=' + value) : nextEnv.trimEnd() + '\n' + key + '=' + value + '\n';
}
writeFileSync(envFile, nextEnv, {mode: 0o600});
if (config) {
  config.Provider.IssuerURL = target;
  config.Provider.BackchannelOrigin = 'http://keycloak:8080';
  writeFileSync(configFile, JSON.stringify(config, null, 2) + '\n', {mode: 0o600});
}
writeFileSync(resolve(directory, 'state.json'), JSON.stringify({version: 1, from, to: target, completedAt: new Date().toISOString()}, null, 2) + '\n', {mode: 0o600});
console.log('Configured the gateway issuer and preserved auth identity ownership.');
