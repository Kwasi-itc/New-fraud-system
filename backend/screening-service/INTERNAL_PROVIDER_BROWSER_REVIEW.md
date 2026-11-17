# Phase 8 browser regression investigation

Completed 2026-11-15 UTC. Existing tasks: FND-02, PVL-03, HARD-01, REL-01. The original run had **111 passed / 21 failed / 1 skipped**. All 21 failures were investigated and repaired. The final entire suite has **134 passed / 0 failed / 1 skipped / 0 flaky** across 135 tests. Two additional tests cover accessible case tabs and identifier-profile source enrichment.

The single full-suite skip requires the separately hosted genuine API/worker handshake. Its separate run **passed 1/1 with no skip** against disposable PostgreSQL, the actual screening API, a freshly built worker and genuine signed/current investigator identity. The auth login/session exchange and unrelated dependencies remain controlled test fixtures; this is not an external OIDC or production-environment test.

## Classification of every original failure

| Original file and test | Classification / cause | Repair and retained behavior |
| --- | --- | --- |
| decision-selection — select decisions, show actual membership, create one case and navigate | Outdated expectation: empty selection toolbar is hidden | Assert hidden toolbar and unchecked rows, then enable selection; retain closed/private membership denial, actual linked case, exact selected IDs, creation and navigation |
| decision-selection — selection clears on filter/page-size changes and modal supports remove/cancel | Outdated expectation: hidden toolbar instead of disabled button; selected count moved to modal | Check actual selected rows and modal count; verify filter/page-size clearing, remove and cancel |
| display-settings — configure captions, linked labels and column order, then view them in Decisions | Outdated controls plus fixture contract problem: column DTO and linked target fields changed; live values now require opt-in fetch | Use current editor, live-field switch and menu filters; fixture implements current display-column contract. Retain saved/reopened labels/order, caption, linked customer value, selection clearing and explicit column hiding |
| display-settings — permission failure keeps the editor and unsaved changes visible | Outdated editor labels/controls and fixture shape | Retain permission denial, visible editor and exact unsaved text; no permission bypass |
| record-explorer — investigator explores a relationship, follows the trail and saves a case note | Outdated tab/record/breadcrumb labels; investigation also uncovered a genuine audit defect | Enter current investigation tab, retain traversal, relationship reason, associated decisions, pagination/attachment and root return. Persist exact decision/record/path identifiers in the saved note and assert them through the API |
| record-explorer — uncertain attachment refreshes membership and revoked exploration hides decisions | Outdated tab/record labels | Retain lost-ack membership refresh, absence of duplicate attach action, revoked-access message and removal of decision data |
| workspace — decision lookup retains closed membership and creates from an unlinked decision | Outdated decision-link label | Locate the actual decision href; retain membership state, create and navigation assertions |
| workspace — investigator records approve with history and no automatic case closure | Outdated tab layout | Select Screening and Activity tabs; retain persisted review/history, reload behavior and open case |
| workspace — investigator records decline with history and no automatic case closure | Outdated tab layout | Same current interactions; retain decline-specific history and no automatic closure |
| workspace — investigator records confirmed_hit with history and no automatic case closure | Outdated tab layout | Retain confirmed-hit-specific persisted state and history |
| workspace — investigator records no_hit with history and no automatic case closure | Outdated tab layout | Retain no-hit-specific persisted state and history |
| workspace — decision screening arrives with its decision (partial empty) | Outdated tab layout | Select Screening; retain decision-linked incomplete evidence and refusal to treat it as clean |
| workspace — decision screening arrives with its decision (match) | Outdated tab layout | Retain actual decision-linked match and review controls |
| workspace — screening arrival is visible before review (partial zero matches) | Outdated tab layout | Retain partial/no-match visibility and correct review availability |
| workspace — screening arrival is visible before review (pending match) | Outdated tab layout | Retain pending match visibility before review |
| workspace — investigator inspects evidence, comments, closes, reloads, and reopens | Outdated tabs/action label plus fixture problem: obsolete execution fields caused a render error | Fixture supplies actual rule execution DTO; retain evidence/score/outcome, comment, closing validation, reload, reopen, events and private controls |
| workspace — empty queues and tenant switches never show another tenant's cached cases | Outdated authority assumption: a query-string tenant is no longer session authority | Assert current authenticated workspace stays authoritative AND explicitly request a foreign-tenant BFF route, requiring 403 and no foreign case data |
| workspace — evidence uploads finalize, survive reload, and download through the private proxy | Outdated Files tab layout | Retain upload/finalization, persisted reload, authorized private download and denial/cache assertions |
| workspace — report drafts retain findings, require completion fields, and export privately | Outdated Files/report tab layout | Retain drafts, required findings, finalization and private export checks |
| workspace — decision transfer confirms destination and safely retries a lost acknowledgment | Outdated decision link/tab layout | Retain explicit destination confirmation, lost-ack retry, membership and current tenant checks |
| workspace — stale closure refreshes state and reopening handles capacity errors | Outdated Files tab navigation after refresh | Retain stale-state refresh, required evidence/files and capacity-error handling |

These are classifications from direct failure messages, current implementation/DTO inspection and successive reruns, not an assertion that every failure was independently reproduced on historical HEAD. Three representative failures had independent unchanged-HEAD browser reproduction in the earlier evidence. No instability-only or unexplained new-regression classification is used to dismiss a failure.

## Product repairs found during investigation

- Case tabs now implement keyboard navigation, roving focus and named tab panels; a new browser check verifies Arrow/Home/End behavior and panel relationships.
- Record headings and open-record controls expose precise accessible names. Connection-trail audit notes retain the root decision, terminal record identity and exact traversal path alongside their human-readable presentation.
- Stored case evidence now permits the supported `identifier-evidence-v1` enrichment profile. A new test verifies the authorized POST, loaded state, preserved leading-zero identifier and persisted reload.
- Fixtures implement the current display and rule-execution contracts. Test interactions follow current tabs, labels, filters and optional live-field fetching; security and persistence assertions remain in place.

No test was deleted, skipped to hide a defect, or stripped of its tenant/permission/review/persistence assertions. Intermediate runs that failed while exposing deeper obsolete controls are investigation evidence, not final passes. The final full run and actual-provider run are recorded separately in the Phase 8 machine manifest.

## Wider frontend checks

The patched Next.js frontend builds and type-checks. Both production-only and full npm dependency audits report zero known vulnerabilities. Next/eslint-config-next are aligned at 16.4.6; a parent-scoped development-only minimatch override addresses the remaining parser dependency advisory.

Full lint still reports **20 errors and 74 warnings**. The same error counts, file/rule classes and warning count reproduce in the earlier unchanged-HEAD frontend checkout using the patched toolchain; affected baseline source text matches HEAD after CRLF normalization. These are recorded separately rather than being misrepresented as browser failures or suppressed by disabling lint rules. They include existing CommonJS script rules, synchronous state-in-effect findings, memoization dependency findings and a prefer-const finding. Broader frontend lint cleanup remains an explicit release/code-quality action; this acceptance task has not silently altered unrelated rule editors or state architecture to erase that debt.
