# Standalone screening provider delivery

Started 2026-11-15. Requested scope: the same provider can run as an independent product and remain integrated with the fraud platform. This extends the deployment boundary; it does not approve commercial source use, matching quality or production activation. The Phase 8 acceptance record remains applicable.

- [ ] Define a versioned synchronous search/enrichment/catalog/freshness/delta API with explicit source/version provenance and incomplete-result semantics.
- [ ] Add provider-owned machine clients, hashed credentials, operation/dataset permissions, revocation, bounded requests and sanitized audit/usage signals. Existing fraud user roles and private two-person approval remain unchanged.
- [ ] Bootstrap a catalog on an empty PostgreSQL database without `core`, `identity`, cases, decisions or River. Reuse canonical source DDL and matching/collection code; verify compatibility with the existing shared catalog.
- [ ] Add a provider-only executable, migration/admin tooling, container and Compose stack. Collection stays a separate private process; no official source activates automatically.
- [ ] Add an authenticated HTTP adapter for fraud workflows, preserving tenant authority, evidence, existing local integration and error-only decision behavior. Make transport/configuration explicit; no fallback on provider failure.
- [ ] Verify independent startup, actual API search/enrichment, private/global grants, revocation, malformed inputs, deadlines/overflow, publication/collection, repeat migrations and shared-schema compatibility on disposable PostgreSQL.
- [ ] Run relevant existing regressions and document endpoint examples, setup/credentials, integration, operations, market-readiness limits and exact remaining acceptance work.

The integrated deployment retains one logical application database. A separately installed provider product must be able to use its own catalog, with no fraud platform runtime dependency. No application database, production source rights, existing credentials or deployed configuration will be changed during implementation/testing.
