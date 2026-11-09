import {appendFileSync} from 'node:fs';

// Preload only in the acceptance runner's children. Record observed responses,
// never request bodies, credentials, or invented/static coverage claims.
if (process.env.DATA_MODEL_ENDPOINT_TRACE) {
  const original = globalThis.fetch;
  const origin = new URL(process.env.DATA_MODEL_LOCAL_ORIGIN ?? 'http://localhost:8080').origin;
  globalThis.fetch = async (input, init) => {
    const url = new URL(input instanceof Request ? input.url : input);
    const method = (init?.method ?? (input instanceof Request ? input.method : 'GET')).toUpperCase();
    const started = performance.now();
    try {
      const response = await original(input, init);
      if (url.origin === origin) {
        appendFileSync(process.env.DATA_MODEL_ENDPOINT_TRACE, JSON.stringify({
          method, path: url.pathname, status: response.status,
          elapsedMs: Math.round(performance.now() - started),
        }) + '\n');
      }
      return response;
    } catch (error) {
      if (url.origin === origin) {
        appendFileSync(process.env.DATA_MODEL_ENDPOINT_TRACE, JSON.stringify({
          method, path: url.pathname, error: error.name,
        }) + '\n');
      }
      throw error;
    }
  };
}
