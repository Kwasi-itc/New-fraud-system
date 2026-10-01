# Fraud operations frontend

Next.js 16.2.7 / React 19 application. Install with `npm ci`, run locally with `npm run dev`, and build with `npm run build`.

The Cases workspace has live queues, paginated case history, investigator actions, evidence views, related cases, inbox SLA/capacity settings, native evidence upload/download, bulk actions with retry results, and tag administration. It uses a same-origin server boundary and signed investigator sessions. The existing login form does not yet establish those sessions; the identity-provider integration is pending provider details.

See the [investigator contract](../backend/case-manager-service/INVESTIGATOR_CONTRACT.md) for required runtime URLs, secure session-cookie configuration, origin protection, API semantics, and remaining deployment checks. Never configure a shared service token as a public browser variable.

Run `npm run build` and `npm run test:cases` (install Chromium with `npx playwright install chromium` first). Browser tests start isolated HTTP fixtures on ports 3199 and 3100 and require a built application. `PLAYWRIGHT_BROWSER_PATH` optionally selects an installed Chromium executable. PostgreSQL and JWT enforcement are covered separately by case-manager integration tests.
