import assert from 'node:assert/strict';
import {test} from 'node:test';
import {digest, importAllowed, realmManifest} from './migration-contract.mjs';
const realm = () => ({realm: 'fraud', id: 'retained-realm', enabled: true,
  users: [{id: 'retained-user', username: 'example', enabled: true, credentials: [{type: 'password', secretData: '{"salt":"private-salt","value":"private-password-hash"}', credentialData: '{"algorithm":"argon2"}'}], realmRoles: ['viewer', 'admin']}],
  clients: [{id: 'retained-client', clientId: 'fraud-auth', enabled: true, secret: 'private-client-secret', redirectUris: ['http://localhost:8088/api/auth/callback']}],
  roles: {realm: [{id: 'role', name: 'viewer'}]}, components: {'org.keycloak.keys.KeyProvider': [{id: 'signing-key', config: {privateKey: ['private-signing-key']}}]},
});
test('identity manifest preserves IDs while hiding all credential and key material', () => {
  const result = realmManifest([realm()]);
  assert.equal(result[0].users[0].id, 'retained-user');
  const text = JSON.stringify(result);
  for (const privateValue of ['private-salt', 'private-password-hash', 'private-client-secret', 'private-signing-key']) assert(!text.includes(privateValue));
});
test('user order, role order and credential JSON ordering do not change identity verification', () => {
  const original = realm(); original.users.push({...original.users[0], id: 'another-user', username: 'another'});
  const changed = structuredClone(original); changed.users.reverse();
  for (const user of changed.users) {user.realmRoles.reverse(); user.credentials[0].secretData = '{"value":"private-password-hash","salt":"private-salt"}';}
  assert.equal(digest(realmManifest([original])), digest(realmManifest([changed])));
});
test('changed identity, password, client secret, callback, roles or signing key fails comparison', () => {
  const expected = digest(realmManifest([realm()]));
  for (const change of [r => r.users[0].id = 'wrong-user', r => r.users[0].credentials[0].secretData = 'changed', r => r.clients[0].secret = 'changed', r => r.clients[0].redirectUris = ['http://wrong.invalid'], r => r.roles.realm[0].id = 'changed', r => r.components['org.keycloak.keys.KeyProvider'][0].config.privateKey = ['changed']]) {
    const changed = realm(); change(changed); assert.notEqual(digest(realmManifest([changed])), expected);
  }
});
test('only an empty target may be imported; retries verify and never overwrite', () => {
  assert.equal(importAllowed(0, false), true);
  assert.equal(importAllowed(2, true), false);
  assert.throws(() => importAllowed(2, false), /refusing to overwrite/);
  assert.throws(() => importAllowed(-1, true));
  assert.throws(() => realmManifest([{realm: 'fraud'}]));
});
test('only absent secrets on known bearer-only builtins may become generated on import', () => {
  const source = realm(); source.clients.push({id: 'builtin', clientId: 'broker', bearerOnly: true});
  const expected = realmManifest([source]);
  const imported = structuredClone(source); imported.clients[1].secret = 'new-unused-builtin-secret';
  assert.equal(digest(realmManifest([imported], expected)), digest(expected));
  imported.clients[0].secret = 'changed-application-secret';
  assert.notEqual(digest(realmManifest([imported], expected)), digest(expected));
  source.clients[1].secret = 'existing-builtin-secret';
  const priorSecret = realmManifest([source]);
  const changedBuiltin = structuredClone(source); changedBuiltin.clients[1].secret = 'changed-builtin-secret';
  assert.notEqual(digest(realmManifest([changedBuiltin], priorSecret)), digest(priorSecret));
  const changedType = structuredClone(source); changedType.clients[1].bearerOnly = false;
  assert.notEqual(digest(realmManifest([changedType], priorSecret)), digest(priorSecret));
  const custom = realm(); custom.clients.push({id: 'custom', clientId: 'custom-resource-server', bearerOnly: true});
  const customBaseline = realmManifest([custom]); custom.clients[1].secret = 'generated';
  assert.notEqual(digest(realmManifest([custom], customBaseline)), digest(customBaseline));
});
