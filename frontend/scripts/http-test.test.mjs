import assert from 'node:assert/strict';
import test from 'node:test';
import {httpTest} from './http-test.mjs';

test('ordinary deployments retain secure cookie behavior', () => {
  assert.equal(httpTest({}), false);
  assert.equal(httpTest({BACKEND_PROFILE:'mysql', APP_ORIGIN:'http://localhost:3002'}), false);
});
test('explicit authenticated HTTP IP test deployment', () => {
  assert.equal(httpTest({EC2_MYSQL_HTTP_TEST:'1', BACKEND_PROFILE:'mysql', APP_ORIGIN:'http://192.0.2.10:13002'}), true);
});
test('invalid flags, origins and anonymous profiles fail closed', () => {
  const valid={EC2_MYSQL_HTTP_TEST:'1', BACKEND_PROFILE:'mysql', APP_ORIGIN:'http://192.0.2.10:13002'};
  for(const change of [
    {EC2_MYSQL_HTTP_TEST:'true'}, {BACKEND_PROFILE:'postgres'}, {LOCAL_MYSQL_NO_AUTH:'1'},
    ...['http://example.com','https://192.0.2.10','http://user@192.0.2.10',
      'http://192.0.2.10/path','http://192.0.2.10/?query=1','http://192.0.2.10/#fragment',''].map(APP_ORIGIN=>({APP_ORIGIN}))
  ]) assert.throws(()=>httpTest({...valid,...change}));
});
