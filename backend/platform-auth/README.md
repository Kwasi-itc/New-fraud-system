# platform-auth

Shared typed JWT verification, capability policies, service signing and transactional current-authority checks. This module replaces `backend/investigator`; its case-review adapter retains domain-owned receipts, history and idempotency.

See the [authentication guide](../../docs/authentication/README.md), [operations](../../docs/authentication/operations.md), and [verification report](../../docs/authentication/verification.md).

`authtest` is a test-only helper package. Production middleware always verifies HTTP credentials even when a principal is present in a request context. The test-only domain router must not be used for cryptographic or missing-credential tests.

Generate the canonical catalog/bundles:

```sh
go run ./cmd/auth-contract > ../../docs/authentication/capability-contract.json
go run ../auth-service/scripts/generate-capability-seed.go > ../auth-service/internal/platform/schema_v5_seed.sql
```
