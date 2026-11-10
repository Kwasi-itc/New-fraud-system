import assert from 'node:assert/strict';
import {test} from 'node:test';
import {prepareDecisionPage,resetDecisionPages} from '../src/lib/decision-pages.ts';
test('existing page controls use opaque owner cursors instead of SQL offsets',()=>{
 resetDecisionPages('session-a');
 const first=prepareDecisionPage('/v1/tenants/a/decisions?limit=25&offset=0&outcome=review');
 assert.equal(new URL(first.path,'http://test').searchParams.has('offset'),false);
 first.record({pagination:{next_cursor:'owner-page-two'}});
 const next=new URL(prepareDecisionPage('/v1/tenants/a/decisions?outcome=review&offset=25&limit=25').path,'http://test');
 assert.equal(next.searchParams.get('cursor'),'owner-page-two');assert.equal(next.searchParams.has('offset'),false);
 for(const path of ['/v1/tenants/b/decisions?limit=25&offset=25&outcome=review','/v1/tenants/a/decisions?limit=25&offset=25&outcome=decline','/v1/tenants/a/decisions?limit=50&offset=25&outcome=review'])assert.equal(new URL(prepareDecisionPage(path).path,'http://test').searchParams.has('cursor'),false);
 resetDecisionPages('session-b');
 assert.equal(new URL(prepareDecisionPage('/v1/tenants/a/decisions?limit=25&offset=25&outcome=review').path,'http://test').searchParams.has('cursor'),false);
});
test('explicit cursors discard legacy offsets and malformed response positions are ignored',()=>{
 const request=prepareDecisionPage('/v1/tenants/a/decisions?limit=25&offset=25&cursor=explicit');
 assert.equal(new URL(request.path,'http://test').searchParams.has('offset'),false);
 request.record({pagination:{next_cursor:{unsafe:true}}});
 assert.equal(new URL(prepareDecisionPage('/v1/tenants/a/decisions?limit=25&offset=50').path,'http://test').searchParams.has('cursor'),false);
});
test('explicit cursor navigation preserves already learned page positions',()=>{
 resetDecisionPages('explicit-navigation');
 prepareDecisionPage('/v1/tenants/a/decisions?limit=25').record({pagination:{next_cursor:'two'}});
 prepareDecisionPage('/v1/tenants/a/decisions?limit=25&cursor=two').record({pagination:{next_cursor:'three'}});
 assert.equal(new URL(prepareDecisionPage('/v1/tenants/a/decisions?limit=25&offset=25').path,'http://test').searchParams.get('cursor'),'two');
 assert.equal(new URL(prepareDecisionPage('/v1/tenants/a/decisions?limit=25&offset=50').path,'http://test').searchParams.get('cursor'),'three');
});
