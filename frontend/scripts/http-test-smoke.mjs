// Run inside the built frontend image with --network none. The auth owner is
// represented by a loopback stub; no real user, provider or database is used.
import assert from 'node:assert/strict';
import {createServer} from 'node:http';
import {spawn} from 'node:child_process';
import {once} from 'node:events';
import {setTimeout as delay} from 'node:timers/promises';

const auth=createServer((req,res)=>{
  assert.equal(req.headers.authorization,'Bearer test-proxy-credential');
  res.setHeader('Content-Type','application/json');
  if(req.url==='/internal/login/start')res.end(JSON.stringify({binding:'test-binding',url:'http://192.0.2.10:18090/authorize'}));
  else if(req.url==='/internal/login/complete')res.end(JSON.stringify({session:'test-session'}));
  else {res.statusCode=404;res.end('{}');}
});
await new Promise(resolve=>auth.listen(8087,'127.0.0.1',resolve));
try {
  for(const remote of [false,true]) {
    const appOrigin=remote?'http://192.0.2.10:13002':'http://localhost:3002';
    const env={...process.env,NODE_ENV:'production',PORT:'3000',HOSTNAME:'127.0.0.1',
      BACKEND_PROFILE:'mysql',APP_ORIGIN:appOrigin,AUTH_SERVICE_URL:'http://127.0.0.1:8087',AUTH_PROXY_TOKEN:'test-proxy-credential'};
    delete env.LOCAL_MYSQL_NO_AUTH;
    if(remote)env.EC2_MYSQL_HTTP_TEST='1';else delete env.EC2_MYSQL_HTTP_TEST;
    const child=spawn(process.execPath,['server.js'],{cwd:'/app',env,stdio:'ignore'});
    const stopped=once(child,'exit');
    try {
      let ready=false;
      for(let attempt=0;attempt<100;attempt++) {
        try {const response=await fetch('http://127.0.0.1:3000/login');if(response.ok){ready=true;break;}}catch{}
        if(child.exitCode!==null)break;
        await delay(100);
      }
      assert.ok(ready,'built frontend did not start');
      const start=await fetch('http://127.0.0.1:3000/api/auth/start',{method:'POST',headers:{Origin:appOrigin},redirect:'manual'});
      assert.equal(start.status,303);
      const loginName=remote?'fraud_ec2_test_login':'__Host-fraud_login';
      const sessionName=remote?'fraud_ec2_test_session':'__Host-fraud_session';
      const login=start.headers.get('set-cookie')??'';
      assert.ok(login.includes(loginName+'=test-binding'));
      assert.ok(login.toLowerCase().includes('httponly'));
      assert.equal(/;\s*secure(?:;|$)/i.test(login),!remote);
      const complete=await fetch('http://127.0.0.1:3000/api/auth/callback?state=test&code=test',{
        headers:{Cookie:loginName+'=test-binding'},redirect:'manual'});
      assert.equal(complete.status,303);
      const session=complete.headers.get('set-cookie')??'';
      assert.ok(session.includes(sessionName+'=test-session'));
      assert.ok(session.toLowerCase().includes('samesite=lax'));
      assert.equal(/;\s*secure(?:;|$)/i.test(session),!remote);
      console.log((remote?'EC2 HTTP test':'ordinary local')+' compiled login cookie flow passed');
    } finally {
      child.kill('SIGTERM');
      await stopped;
    }
  }
} finally {
  await new Promise(resolve=>auth.close(resolve));
}
