import assert from 'node:assert/strict';
import {test} from 'node:test';
import {issuerURLs} from './urls.mjs';
test('provider administration and discovery retain their context path', () => {
  const urls = issuerURLs('http://localhost:8088/identity/realms/fraud');
  assert.equal(urls.masterToken, 'http://localhost:8088/identity/realms/master/protocol/openid-connect/token');
  assert.equal(urls.clients, 'http://localhost:8088/identity/admin/realms/fraud/clients');
  assert.equal(issuerURLs('http://localhost:8090/realms/fraud').base, 'http://localhost:8090');
  for (const value of ['http://remote.invalid:8088/identity/realms/fraud','http://localhost:8088/identity/realms/master','http://user@localhost:8088/realms/fraud','http://localhost:8088/realms/fraud?x=1']) assert.throws(() => issuerURLs(value));
});
