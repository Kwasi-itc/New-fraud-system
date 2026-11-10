import {isIP} from 'node:net';

// Explicit remote HTTP testing only. Ordinary/local/production cookie defaults
// remain secure. A misspelled flag or unexpected origin must not downgrade them.
export function httpTest(env=process.env) {
  if (!env.EC2_MYSQL_HTTP_TEST) return false;
  if (env.EC2_MYSQL_HTTP_TEST!=='1'||env.BACKEND_PROFILE!=='mysql'||env.LOCAL_MYSQL_NO_AUTH) {
    throw new Error('Explicit authenticated EC2 HTTP test configuration required');
  }
  const origin=new URL(env.APP_ORIGIN??'');
  if(origin.protocol!=='http:'||!isIP(origin.hostname)||origin.username||origin.password||origin.search||origin.hash||origin.pathname!=='/') {
    throw new Error('EC2 HTTP testing requires an exact HTTP IP origin');
  }
  return true;
}
