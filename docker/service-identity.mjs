import { createPrivateKey, randomUUID, sign } from 'node:crypto';
import { readFileSync } from 'node:fs';
const keys = new Map();
// Command-line callers hold only their own private key. The receiving Go
// verifier enforces the key's service, audience and maximum capability binding.
export function serviceBearer(file, kid, service, audience, capabilities, tenant = '00000000-0000-0000-0000-000000000000') {
  let key=keys.get(file);
  if (!key) { key=createPrivateKey(readFileSync(file)); keys.set(file,key); }
  if (key.asymmetricKeyType!=='rsa' || key.asymmetricKeyDetails.modulusLength<2048 || !kid || !service || !audience || !capabilities.length) throw new Error('Invalid service signing configuration.');
  const encode=value=>Buffer.from(JSON.stringify(value)).toString('base64url');
  const now=Math.floor(Date.now()/1000);
  const input=`${encode({alg:'RS256',typ:'at+jwt',kid})}.${encode({iss:`urn:fraud:service:${service}`,sub:service,service_id:service,principal_type:'service',tenant_id:tenant,aud:audience,capabilities,orig_type:'service',orig_id:service,iat:now,exp:now+60,jti:randomUUID()})}`;
  return `${input}.${sign('RSA-SHA256',Buffer.from(input),key).toString('base64url')}`;
}
