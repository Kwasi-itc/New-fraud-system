# Staged decision identity and case contracts

This package implements the decision permission matrix approved on 2026-11-03.
It does not change the configured PostgreSQL runtime or provision live identities.

Install `mysqlidentity.Plan` before `case-manager-service/mysqlaccess.Plan`, in
the same logical database as the decision owner. Each owner has an independent
migration ledger. Plans require a trusted reference artifact; destination drift
must not be adopted as the reference. Decision table migrations retain their own
ordering and ownership. These staged MySQL objects do not authorize an application
database architecture change.

Auth owns users, membership roles/versions, sessions and `identity_tenants`.
The latter is an access activation registry, distinct from data-model metadata;
coordinated tenant provisioning and deployment compatibility remain D20 work.
Case owns inboxes, tags and inbox membership, with tenant-consistent identity
references. No decision code directly mutates these owner tables.

Apply each owner's definer grants separately from `RuntimeGrants`. Runtime grants
permit only the required public functions and credential-verification columns;
list reads/mutations need separate decision-owner table grants. Tests install
restricted accounts and verify that cross-owner writes fail. The internal role
bitmask is a versioned owner contract, not a caller-supplied permission assertion.

`sessioncredential` preserves the existing 32-byte opaque-secret format and
SHA-256 digest. `Verifier.Resolve` uses the digest index; it does not issue sessions
or renew idle expiry. `mysqlruntime.BindSessionIdentity` binds the verifier to the
same pool/database and installs a staged bearer-session boundary. It rejects
missing/forged credentials and tenant mismatches. The browser gateway still owns
cookie and CSRF policy. Authorization rechecks current owner state inside the
operation transaction; writes lock user, membership, session and tenant in order,
then check wall-clock expiry after waits. Case batches lock only requested rows
in sorted order and recheck session expiry after waiting.

Approved roles: viewer reads; builder reads/authors/mutates lists; publisher
reads/publishes; platform admin has those actions and may access browser workflows
only with case access. Tenant/case roles alone grant no decision action. Roles
combine subject to tenant, session and inbox restrictions. Unknown actions deny.
Read-only sessions cannot write. Source PostgreSQL browser-read permissions remain
unchanged; the stricter target browser-read policy was explicitly approved.

Service principals require their own explicit tenant/action bindings. This user
session adapter grants neither generic persistence nor execution mutations and
does not manufacture worker authority. Provisioning, live composition, complete
browser edit/copy parity and release acceptance remain separate checklist gates.

Verification and exact commands: [cycle 6 evidence](../../../mysql_migration/implementation/evidence/2026-11-03_decision_cycle6.json).
