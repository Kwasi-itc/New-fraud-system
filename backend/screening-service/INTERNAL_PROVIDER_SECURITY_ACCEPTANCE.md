# Phase 8 security acceptance review

Reviewed 2026-11-15 UTC. Existing tasks: HARD-01, PVL-01/02, INL-02/03, DB-01, OPS-01/02, REL-01. **Development controls are verified; production security acceptance remains pending.** No production credentials, grants, migrations, source activation or release approval were changed.

## Boundaries exercised

| Boundary | Evidence and result | Limit |
| --- | --- | --- |
| Tenant/provider authority | Complete screening and companion suites exercise signed/current identities, revoked memberships/sessions, parent consistency, foreign dataset/case denial, unknown/unconfigured provider rejection and original saved provenance | Trusted service DB roles remain a security boundary; this is not per-tenant PostgreSQL RLS |
| Private import/approval | Tenant administrators import and approve; a different person is required. Actual generated LOGIN roles publish 5,001 fictional records, reject self-approval and foreign-tenant administration | Production role membership and secret provisioning still need review |
| Database privilege | Reader cannot mutate source data; private writer cannot enable collection; restricted roles cannot change identity membership; unprivileged login cannot read source records or execute owner statistics maintenance. Wrong DB passwords fail real authentication | Disposable generated credentials on a local network; not production TLS/firewall/secret rotation qualification |
| Same database/primary | Reader/writer binding compares identity UUID plus actual DB/server/primary attributes. A restored clone with the same copied identity UUID is rejected when paired with the original; the same restored DB is accepted | DNS/proxy/failover topology must be rehearsed in the deployment |
| Source intake | Existing hostile XML/DTD/entity, truncation, bounded download/parse, registry/SSRF/redirect and artifact integrity tests pass; full retained official snapshots are reverified | External TLS/egress policy, source rights and production supply chain remain separate |
| Evidence/audit | Immutable decision snapshots and generation attribution survive source updates, rollback and actual restore. Connection notes now retain exact root/record/path identifiers | Broader institution audit retention/export and incident custody require approval |
| Worker authority/recovery | Actual API/worker journeys exercise trusted intake/callback boundaries, private job ownership, revoked access, retries, fenced publishers and recovery | A compromised trusted capability is high-impact; deployment identities/network must be restricted |
| Inline failure | Approved error-only policy: failure, timeout, stale or incomplete screening produces an evaluation error with no successful decision/effects; multi-scenario rollback tested | Production timeout/SLO and per-scenario activation remain explicit approvals |
| Browser/BFF | Entire suite passes, with explicit foreign-tenant 403, private downloads, same-origin mutations, no secret/raw-source projection and real provider enrichment/history/monitoring proof | Controlled identity exchange in browser tests is not external OIDC penetration testing |
| Delivery | Final 10,000-decision run verifies 10,000 workflow and 30,000 outbox HTTP receipts, zero retry/discard/incomplete/error and no internal credential forwarded to an outbound workflow URL | Controlled receivers do not qualify arbitrary real third-party endpoints or exactly-once networking |

## Findings and remediation

| Finding | Severity / state | Remediation / verification |
| --- | --- | --- |
| Next 16.2.7 production dependency advisories, including critical RCE advisories | **Critical/high — patched** | Next and eslint-config-next updated to 16.4.6; compatible transitive patches applied; production build and complete browser suite pass. [Next release](https://github.com/vercel/next.js/releases/tag/v16.4.6), [cache RCE advisory](https://github.com/advisories/GHSA-673q-mjm6-r5xr), [image SVG advisory](https://github.com/advisories/GHSA-2464-fhr4-w9xw) |
| Remaining development-only minimatch parser advisory | **Moderate — patched** | Narrow parent-scoped override to patched minimatch 11; production and full npm audits now report zero known findings. Patched ESLint executes and reproduces the existing HEAD lint findings without new error classes. [Advisory](https://github.com/advisories/GHSA-g3hv-75wg-m249) |
| A copied DB identity could let a misconfigured reader/writer pair point at different restored databases | **Medium engineering finding — fixed** | Same-primary binding contract added, actual restored-clone denial and restricted writer acceptance pass; no new source of tenant authority |
| Human-readable connection audit omitted exact decision/record/path identities | **Medium audit-trace finding — fixed** | Precise identities persisted alongside readable note; browser asserts saved API audit content |
| New statistics maintenance might require excessive DB rights | **Preventive review — narrowed and verified** | Migration 22 fixes search path/targets, revokes PUBLIC and exposes only EXEC to reviewed service capabilities; actual login negative/large-publication tests pass |

Four source-mode govulncheck scans use Go 1.27.4, scanner 1.9.0 and the recorded vulnerability DB snapshot. They report **no reachable function-level findings**. Screening, decision and case still report module/package-level advisories (including GO-2026-6860/6866/5932) without a reachable function trace; auth has none. These are retained in the machine evidence, not relabelled as a wholly empty dependency scan. Recheck on the exact release binary/image and new dependency/toolchain updates.

## Unresolved acceptance matters

- **Unassessed severity:** container base/OS image scan, immutable digests, signed artifacts/SBOM, deployment penetration testing and exposed network/TLS/egress review have not been supplied. No clean-image attestation is claimed.
- **High-impact deployment boundary:** service DB capabilities and machine tokens are trusted. Provision isolated least-privilege logins, protected mounted secrets, restricted network access, rotation and incident revocation under a named security owner. The test accounts were generated only in disposable databases.
- **Compliance/privacy release gate:** source-specific commercial/reuse/personal-data/retention decisions are absent. A passing parser or security test cannot grant source permission.
- **Quality/risk release gate:** name quality fails and identifier quality lacks representative independent acceptance. Exact IDs/name aliases create review evidence, not proven identity or guilt.
- **Operational release gate:** deployment-size encrypted backup/PITR/key recovery, alert delivery/on-call, capacity and explicit activation remain unaccepted.
- **Existing frontend code-quality debt:** full lint has 20 errors/74 warnings, reproduced with identical file/rule classes on unchanged HEAD. No rule was disabled to hide them; a separately reviewed cleanup/release decision remains required.

The [source approval register](INTERNAL_PROVIDER_SOURCE_APPROVALS.md), [matching report](INTERNAL_PROVIDER_MATCHING_EVALUATION.md), [recovery verification](INTERNAL_PROVIDER_RECOVERY.md) and [activation runbook](INTERNAL_PROVIDER_ACTIVATION.md) identify the exact owners/actions. This is an engineering security review, not independent institutional sign-off.
