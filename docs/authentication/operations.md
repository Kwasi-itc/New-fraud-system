# Migration and key operations

## Coordinated rebuild

Do not roll out mixed old/new authentication binaries. All application services share the same PostgreSQL database; this change introduces no additional application database. Preserve the existing identity provider, accounts, memberships, sessions, invitations, domain histories and PostgreSQL volume.

1. Back up the shared database and private identity directory. Stop API/worker writes for the migration/rebuild window. Run the existing identity/legacy-actor preflight before changing persisted references; arbitrary legacy actor strings are not converted into trusted identities.
2. Generate per-service signing identities using the existing platform/provider material:

   ```sh
   node backend/auth-service/scripts/setup-local-provider.mjs
   node backend/auth-service/scripts/setup-platform.mjs
   ```

   Set `PLATFORM_AUTH_DIRECTORY` when using a different existing private directory, and `AUTH_TOKEN_ENDPOINT_URL` to the public `/oauth/token` URL. The local gateway setup sets its own URL; EC2 configuration sets the HTTPS origin. An incomplete key/environment pair fails rather than replacing a persisted key silently. Existing provider and platform private keys are preserved. New service keys are distinct RSA-3072 pairs; private files are mode 0600. Only public material belongs in the trust ring.
3. Apply independently tracked migrations in this order: Data Model metadata, identity through v5 plus its generated seed, ingestion, decisions, screening through 24, then Case Manager through 15. Identity depends on `core.tenants`; the new Case Manager bouncer/ACL migration requires identity v5 and its domain tables. Existing prerequisites remain. Compose migration dependencies implement this ordering. Auth's owner migration runner executes the structure and seed in one transaction and rejects schemas newer than its binary.
4. Rebuild all backend APIs/workers and the frontend. Mount the shared public ring and **only the process's own private key** into each API/worker. Auth-service alone receives the platform private key. The frontend receives only its BFF private key; its entrypoint copies that leaf into a private runtime directory before dropping to `node`.
5. Validate readiness, login/session resolution, a tenant-scoped queue/review, client proof exchange/ingestion, and signed outbox delivery before restoring traffic. Review the current registered route inventory and predefined capability bundles. Do not regard Compose rendering or unit tests as deployment validation.

Identity v5 adds normalized tenant role/member/invitation references, capability definitions, clients, public keys and scope approvals. It backfills existing assignments and pending invitations. Case Manager 15 adds tenant-consistent client/inbox grants, a bounded-page index and the single case bouncer. Screening 24 adds outbox originator metadata with `{}` for historical rows. Previous case-review receipts remain immutable and support exact replay.

Migrations use bounded lock/statement timeouts and transactional changes. The client-case index and identity backfill can lock large existing tables; measure migration duration on a representative restored database and use the coordinated maintenance window. They are not concurrent index builds. A failed migration rolls back; do not erase migration versions or force partially applied DDL into success. If restoring a prior deployment, restore the matching database and private identity state together. A binary downgrade alone is not a safe rollback after custom roles/client grants have been used.

Non-owner database roles need explicit schema usage/read privileges and EXECUTE on the owner contracts they consume. In particular, `identity.authorize_capability_v1(text,uuid,uuid,bigint,text,boolean)` and `case_manager.authorize_case_access_v1(uuid,uuid,text,text,text,uuid,bigint,boolean,boolean,text)` revoke PUBLIC execution. Auth alone needs member capability resolution and identity mutation ownership. Existing private-writer/notice-operator grants are updated in `backend/screening-service/scripts/source-role-grants.sql`. Grant only the contracts required by the configured database role; do not grant arbitrary cross-service mutation rights.

## Runtime configuration

Each API/worker profile supplies:

```text
PLATFORM_AUTH_KEYS_FILE=/run/secrets/public-keys.json
PLATFORM_AUTH_AUDIENCE=urn:fraud:receiving-service
PLATFORM_AUTH_SIGNING_KEY_FILE=/run/secrets/own-service-private.pem
PLATFORM_AUTH_SIGNING_KEY_ID=own-unique-key-id
PLATFORM_AUTH_SERVICE_ID=own-service-identity
```

Native host execution needs absolute host paths instead of container `/run/secrets` paths. Startup validates the trust ring and the process's private key against its trusted service identity. The auth issuer uses `PLATFORM_JWT_*` and `AUTH_TOKEN_ENDPOINT_URL` from `platform.env`. BFF transport requires `frontend-bff`; a backend service key cannot replace it. An unavailable/misconfigured signer fails the call, without static fallback.

`PLATFORM_ACCESS_TOKEN` in operator/replay harnesses means an actual typed access token with the appropriate audience and capabilities. The harness does not refresh it automatically; arrange renewal for runs longer than the token lifetime. Do not supply a retired shared secret.

## Signing-key rotation

Trust and private keys are loaded at startup. Rotation uses overlapping public keys and coordinated process restarts:

1. Generate a new private key under a **new key ID** for the same service/issuer/type binding. Add its public entry with the same permitted capabilities/audiences while retaining the old public entry. Distribute the ring and restart receivers so both keys are accepted.
2. Update that service's private leaf and environment key ID as a matching pair, then restart its processes. Never copy one service's private key to another. Auth issuer rotation follows the same process, preserving the platform user/client issuer binding.
3. Wait out every token accepted under the old key plus leeway: 65 seconds for human/service keys; **60 minutes plus five seconds** for the platform key because verifiers permit client tokens up to that bound. Remove the old public entry and restart receivers. Active client exchanges currently issue 15-minute tokens, but using the maximum bound avoids assuming all prior tokens used that default.

Provisioning preserves overlapping historical public entries. It rejects a changed private key under an existing ID. `validatePlatformKeys`/EC2 `check` verifies current keys, identity/type/capability/audience bindings without modifying state. Removing a compromised public key and restarting receivers invalidates its tokens immediately; ordinary client/key revocation in the identity database affects issuance only.

Client-owned key rotation uses the API client page: add a second public key, switch the customer's proof signer, then revoke the old key. A revoked client key ID/fingerprint is not silently resurrected; register a new key identity. Sensitive approvals and role changes are audited transactionally.

## Database-wide resource budget

Calculate the connection ceiling across **all APIs, workers, optional private/provider pools, migrations, Keycloak and operations sessions**, multiplying each pool maximum by its replica count. Read/worker pools count separately when configured separately. Share the same PostgreSQL CPU/I/O and lock budget; a new auth-service replica also requires replacing the local replay cache with shared state.

The EC2 configuration uses a four-connection shared DSN, explicit ingestion/decision maxima of four, Case Manager API/worker maxima of four, and two connections for the case-delivery worker. Auth currently fixes its pool at five. Review optional pools and provider profiles before enabling them; these settings are bounds, not a throughput claim. The real PostgreSQL integration suite also exercises five owner pools capped at two connections each and lock/cancellation behavior. No new broad tenant-row lock is used to serialize unrelated case work. Identity role/key administration uses narrower locks or per-client/operation advisory locks.

## Removal log

- Removed `backend/investigator`; all consumers and image build contexts use `backend/platform-auth`.
- Removed general static-token and separate workflow/screening middleware, bypass configuration fields, and obsolete client token constructor parameters.
- Retired `SERVICE_AUTH_MODE`, `SERVICE_AUTH_TOKEN`, `AUTH_PROXY_TOKEN`, `DATA_MODEL_SERVICE_TOKEN`, case integration secrets/allowlists, workflow/screening authoring/intake/callback/operator platform tokens, `USER_REQUIRE_SESSION` and old `USER_JWT_*` receiving configuration. They cannot enable unauthenticated operation.
- Removed the frontend static-secret helper and workflow static-secret setup script. The workflow runtime probe checks signed-identity boundaries. Root-tenant setup signs as `platform-admin-cli` with only provisioning authority.
- Updated Compose, EC2/local setup, Dockerfiles, route contracts, capability-aware administration and proof exchange. Historical handoff notes are labeled as superseded; they do not describe a supported compatibility mode.
- Retained independent OIDC, invitation, third-party provider and customer callback credentials. They serve different protocols and are not replaced by internal platform JWTs.


## Fraud-system client audience update

External client access tokens use the canonical `urn:fraud:system` audience. Their approved scopes can span APIs, using one registered customer key pair and one access token. The former `AUTH_CLIENT_AUDIENCES` allowlist and per-API scope filtering have been removed. Human tokens retain `urn:fraud:platform-api`; internal service keys/tokens keep their narrowly bound service audiences and capabilities.

No database migration or customer key replacement is required. Rebuild all backend APIs/workers against the updated shared verifier, regenerate public trust/settings with the existing platform-key provisioning script, and restart receivers and auth-service together. Provisioning preserves existing private keys and client registrations. Newly generated platform public trust permits the human and fraud-system audiences; existing customer proofs still target the exact public `/oauth/token` URL. Previously issued API-specific client access tokens are rejected by the new receivers, so clients must exchange a fresh proof for a new token. Preserve the local HTTP cookie settings while recreating the stack.


## Account schema v6 and coordinated upgrade

Build auth-service, the frontend, every receiving API and every worker against the updated shared verifier before restoring traffic. Stop application receivers/issuers while keeping PostgreSQL and the identity provider available; save private configuration/key material and a database backup first. Run the identity-owned migration before dependent services start. Preserve the retained shared database volume and all existing signing keys. Readiness requires identity schema v6.

Schema v6 consolidates each existing membership and pending invitation to one role with exactly the previous capability set. It reuses an equivalent tenant role or creates an explicit tenant-specific migrated bundle, and audits every conversion. It does not increase membership authorization versions or invitation/delivery generations because permissions do not change; existing asynchronous provenance and pending mail remain usable. Historical accepted/cancelled invitations retain their original audit data. The constraints prevent multiple normalized role assignments, and new API calls require exactly one role. Migration failures roll back atomically. DDL/index duration on a large restored application database still needs deployment-specific measurement.

Configure `AUTH_MANAGE_SIGN_IN_URL` to the provider account console; the local gateway uses `http://localhost:8088/identity/realms/fraud/account/`. HTTPS deployment uses its configured public origin. Keep existing session-cookie transport settings. Provider account names and emails appear after a new verified login; existing sessions are deliberately not associated with an arbitrarily chosen external identity. Own display names use a separate optimistic profile version and do not alter provider login credentials.
