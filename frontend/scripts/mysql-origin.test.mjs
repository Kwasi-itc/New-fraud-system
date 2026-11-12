import assert from 'node:assert/strict';
import {test} from 'node:test';
import {localOrigin} from './mysql-origin.mjs';
test('allows legacy and unified local callback origins', () => {
  for (const port of [3002, 8088, 8099]) assert.equal(localOrigin(`http://localhost:${port}/`), `http://localhost:${port}`);
});
test('rejects remote, insecure cookie host aliases and non-origin input', () => {
  for (const value of ['http://example.com:8088', 'http://127.0.0.1:8088', 'https://localhost:8088', 'http://localhost', 'http://localhost:80', 'http://user@localhost:8088', 'http://localhost:8088/path', 'http://localhost:8088?next=bad', 'http://localhost:8088/#bad']) {
    assert.throws(() => localOrigin(value));
  }
});
