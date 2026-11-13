import {randomBytes} from 'node:crypto';
import {chmodSync, existsSync, lstatSync, mkdirSync, readFileSync, writeFileSync} from 'node:fs';
import {resolve} from 'node:path';

export function readEnv(file) {
  if (!existsSync(file)) return {};
  return Object.fromEntries(readFileSync(file, 'utf8').split(/\r?\n/).filter(line => line && !line.startsWith('#')).map(line => {
    const at = line.indexOf('='); return [line.slice(0, at), line.slice(at + 1)];
  }));
}
export function writeEnv(file, values) {
  writeFileSync(file, Object.entries(values).map(([key, value]) => `${key}=${value}\n`).join(''), {mode: 0o600});
}
function secret(directory, name) {
  const file = resolve(directory, name);
  if (existsSync(file)) {
    if (!/^[a-f0-9]{64}$/.test(readFileSync(file, 'utf8'))) throw Error(`Invalid retained credential: ${name}; restore it rather than resetting the database.`);
  } else writeFileSync(file, randomBytes(32).toString('hex'), {flag: 'wx', mode: 0o600});
}
export function prepareFoundation(root, existingVolume = false) {
  const local = resolve(root, '.local/mysql-dev');
  mkdirSync(resolve(local, 'plans'), {recursive: true});
  for (const name of ['root-password.txt', 'data-model-password.txt']) {
    if (existingVolume && !existsSync(resolve(local, name))) throw Error(`Restore ${name} for the existing MySQL volume; no replacement credential was generated.`);
    secret(local, name);
  }
  const file = resolve(local, 'compose.env');
  if (!existsSync(file)) writeEnv(file, {MYSQL_DEV_PORT: 3307});
}
export function prepareApplicationFiles(root) {
  const local = resolve(root, '.local/mysql-dev');
  for (const name of ['ingestion-api-password', 'ingestion-worker-password', 'ingestion-runtime-token', 'decision-api-password', 'decision-worker-password', 'decision-runtime-token', 'decision-review-session', 'decision-provider-password', 'decision-provider-token']) secret(local, name + '.txt');
  for (const owner of ['ingestion', 'decision']) mkdirSync(resolve(local, owner + '-runtime'), {recursive: true});
}

// Compose file secrets are bind mounts: their host modes apply in the container.
// Keep the parent private to the host user while allowing explicitly mounted
// files to be read by the distinct non-root runtime UIDs. Never chmod exports,
// private backups or arbitrary files, and reject symlinks before changing modes.
export function prepareMountPermissions(root, platform = process.platform) {
  if (platform === 'win32') return;
  function mode(file, value) {
    if (!existsSync(file)) return;
    const stat = lstatSync(file);
    if (stat.isSymbolicLink()) throw Error('Refusing to change permissions through a local-state symlink');
    // Installer-written runtime JSON is already 0644 and can be root-owned on
    // native Linux. Avoid a needless chmod by the unprivileged host operator.
    if ((stat.mode & 0o777) !== value) chmodSync(file, value);
  }
  const local = resolve(root, '.local/mysql-dev');
  mode(resolve(root, '.local'), 0o700);
  mode(local, 0o700);
  mode(resolve(root, '.local/auth-provider'), 0o700);
  const sources = ['docker/compose.yaml', 'docker/provider.compose.yaml'].map(file => readFileSync(resolve(root, file), 'utf8')).join('\n');
  const names = new Set([...sources.matchAll(/file:\s+\.\.\/\.local\/mysql-dev\/([a-zA-Z0-9.-]+)/g)].map(match => match[1]));
  for (const name of names) mode(resolve(local, name), 0o644);
  for (const owner of ['auth', 'case', 'screening', 'ingestion', 'decision']) {
    const directory = resolve(local, owner + '-runtime');
    mode(directory, 0o755);
    mode(resolve(directory, 'config.json'), 0o644);
    mode(resolve(directory, 'auth-public.pem'), 0o644);
  }
  // Migration artifacts are read only by root operator/author containers. Keep
  // their original ownership and private modes, including upgrade backups.
}
