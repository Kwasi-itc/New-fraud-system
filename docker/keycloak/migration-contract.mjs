import {createHash} from 'node:crypto';
export function canonical(value) {
  if (Array.isArray(value)) return value.map(canonical);
  if (value && typeof value === 'object') return Object.fromEntries(Object.keys(value).sort().map(key => [key, canonical(value[key])]));
  return value;
}
export const digest = value => createHash('sha256').update(JSON.stringify(canonical(value))).digest('hex');
const sorted = values => [...(values ?? [])].map(canonical).sort((a, b) => JSON.stringify(a).localeCompare(JSON.stringify(b)));
const unordered = value => Array.isArray(value) ? sorted(value.map(unordered)) : value && typeof value === 'object' ? Object.fromEntries(Object.entries(value).map(([key, item]) => [key, unordered(item)])) : value;
const structuredSecret = value => {
  if (!value) return null;
  try { return JSON.parse(value); } catch { return value; }
};

// Compare identity and authentication material, not serialization order or
// generated defaults. No passwords, password hashes or private keys leave this
// function: credential, client-secret and signing-key material is digested.
export function realmManifest(realms, baseline = []) {
  if (!Array.isArray(realms) || !realms.length) throw Error('A complete realm export is required');
  const seen = new Set();
  const builtins = new Set(['broker', 'realm-management', ...baseline.map(realm => realm.realm + '-realm')]);
  return realms.map(realm => {
    if (!realm.realm || !realm.id || seen.has(realm.realm) || !Array.isArray(realm.users)) throw Error('Incomplete or duplicate exported realm');
    seen.add(realm.realm);
    return {
      realm: realm.realm, id: realm.id, enabled: Boolean(realm.enabled),
      users: sorted(realm.users.map(user => ({
        id: user.id, username: user.username, enabled: Boolean(user.enabled),
        email: user.email ?? '', emailVerified: Boolean(user.emailVerified),
        realmRoles: sorted(user.realmRoles), clientRoles: canonical(Object.fromEntries(Object.entries(user.clientRoles ?? {}).map(([client, roles]) => [client, sorted(roles)]))),
        groups: sorted(user.groups), requiredActions: sorted(user.requiredActions),
        credentials: sorted((user.credentials ?? []).map(credential => ({
          type: credential.type, temporary: Boolean(credential.temporary),
          material: digest({secret: structuredSecret(credential.secretData), parameters: structuredSecret(credential.credentialData)}),
        }))),
      }))),
      clients: sorted((realm.clients ?? []).map(client => {
        const prior = baseline.find(value => value.realm === realm.realm)?.clients.find(value => value.clientId === client.clientId);
        // Keycloak generates a secret when importing its built-in bearer-only
        // clients even though their original export has none. Accept only this
        // narrow absence-to-generated transition. Existing secrets and every
        // application client's secret remain strict equality checks.
        const generatedBuiltin = builtins.has(client.clientId) && client.bearerOnly === true && prior?.bearerOnly === true && prior.secret === null;
        return {
          id: client.id, clientId: client.clientId, enabled: Boolean(client.enabled),
          bearerOnly: Boolean(client.bearerOnly), publicClient: Boolean(client.publicClient),
          standardFlowEnabled: Boolean(client.standardFlowEnabled), directAccessGrantsEnabled: Boolean(client.directAccessGrantsEnabled), serviceAccountsEnabled: Boolean(client.serviceAccountsEnabled),
          secret: generatedBuiltin ? null : client.secret ? digest(client.secret) : null,
          redirectUris: sorted(client.redirectUris), webOrigins: sorted(client.webOrigins),
        };
      })),
      roles: digest(unordered(realm.roles ?? {})),
      signingKeys: digest(sorted(realm.components?.['org.keycloak.keys.KeyProvider'])),
    };
  }).sort((a, b) => a.realm.localeCompare(b.realm));
}

export function importAllowed(realmCount, hasMigrationJournal) {
  if (!Number.isInteger(realmCount) || realmCount < 0) throw Error('Invalid target realm count');
  if (realmCount === 0) return true;
  if (!hasMigrationJournal) throw Error('The target MySQL database already contains realms; refusing to overwrite them');
  // A resumed migration must verify its existing import, never overwrite it.
  return false;
}
