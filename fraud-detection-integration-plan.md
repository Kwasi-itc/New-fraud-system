# IT Consortium Fraud Detection System — Integration Plan

**Status:** Draft for review (rev 3) · **Branch:** `fraud-detector` · **Date:** 2026-09-16
**Sources:** `_py_reference_/fraud_detective/NewFraudguide.md`, the integration brief transcripts.
The investigations in [docs/fraud/](fraud/) are used as *worked examples* — they are one pattern
among many, not the basis of the design.

---

## 1. What we are actually building

Prove that the fraud detection system tracks what we send it, and that its rules fire on real
transaction behaviour — measurably, against a set of tests we can run on demand.

That is the whole of phase 1. The brief's framing was *"I want something to test to make sure that
their system will track"*; callback tracking was raised as one candidate way to produce such a test,
not as a separate deliverable. The test cases in §8 serve the same purpose without it, so this plan
does not build a callback-alerting path.

### Three constraints that shape everything below

**We cannot change the fraud detection system.** Every decision has to work with the v1 platform
exactly as documented — no new trigger types, no new endpoints, no protocol changes. The one lever
we genuinely have is the *content* we send: the data model and the scenario/rule definitions, which
v1 explicitly invites us to specify. That lever is where the effort should go.

It also means the rule-change loop is slow — a weight or threshold adjustment is a request to
another team, not a config edit. So we send a **broad rule set up front and tune weights**, rather
than drip-feeding new rules as ideas arrive.

**We are not changing the transaction status checker.**
[`check-transaction-status-worker.ts`](../src/jobs/check-transaction-status-worker.ts) and
[`checkTransactionStatusService`](../src/services/core/checkTransactionStatusService.ts) are out of
scope entirely — no emits, no scheduling, no new producers on `CHECK_TRANSACTION_QUEUE`. Recovery
and reconciliation behaviour stays exactly as it is.

**We should change as little else as possible.** The transaction path is live money. This plan adds
one publisher, one worker, and a set of emit calls placed after writes that already happen. It adds
no new classification logic and no parallel state machine.

---

## 2. What this does and does not detect

Every evaluation path in the guide is *trigger-on-ingest*: decisioning happens when we POST
`record-ingested`, and only then. There is no timer, no TTL, no "record has been in state X for N
minutes" trigger. That is fixed, so the design question is **what we ingest, and when, so that a
plain on-ingest rule answers something useful.**

### Detected

Everything that is a *thing happening*, which is every fraud family in §5:

- amount and velocity patterns, evaluated as each debit reaches a terminal state
- destination concentration, evaluated on each credit leg
- instrument risk, evaluated when a wallet is attached
- account-lifecycle risk, evaluated when a debit is initiated
- **late** callbacks — the callback arrives, we compute `seconds_to_callback` at emit time, and a
  rule fires if it is over threshold

### Not detected

**A callback that never arrives at all.** Nothing is ingested, so nothing is evaluated. Detecting
absence needs either a platform-side timer (which we cannot add) or something on our side driving
the clock (deliberately out of scope, per §1).

Two things are worth being clear about rather than leaving implicit:

1. **The existing watchdog still covers this, unchanged.**
   `UniwalletService.processTransaction` arms `NO_CALLBACK_AFTER_PAYMENT_INITIATION` and
   `DEBITED_BUT_NO_SERVICE` through [`SchedulerHelper`](../src/helpers/scheduler.ts#L68), and the
   support backend alerts on them. That path is untouched by this plan and keeps working exactly as
   it does today.
2. **The data is still queryable even though no rule fires on it.** We emit
   `awaiting_callback` on every leg, so a leg that never resolves simply stays in that state on the
   platform. The guide's read endpoints answer it directly:

   ```
   GET /v1/tenants/{tenantId}/records/transactions/search?field=state&value=awaiting_callback
   ```

   That is a query, not an alert — but it means the information is present on the platform if we
   later want to act on it.

**If the absent-callback alert is wanted as a product feature rather than a test**, it is a separate
piece of work and should be scoped as one. Flagging it here so it is a decision rather than an
omission.

---

## 3. Architecture

```
 business code (unchanged behaviour)
        │
        │  FraudEventPublisher.publish()   ← fire-and-forget, never throws
        ▼
   SQS  FRAUD_EVENT_QUEUE ──────────────┐
        │                                │ DLQ after 3 attempts
        ▼                                │
 transflow-online-fraud-event-worker     │
        │ 1. POST :8087 /ingest/{type}   │  Idempotency-Key = object_id:state
        │ 2. POST :8088 /record-ingested │  mode: async
        ▼
   fraud platform
        │  callback_url
        ▼
  POST /callback/fraud-decision  → log one line per scenario → (phase 2) enforce
```

### Non-negotiable properties

1. **Never on the critical path.** The publisher pushes to SQS and returns. Every call site is
   wrapped so a fraud failure cannot fail a debit. The fraud service is a raw IP on plain HTTP with
   no published SLA — it is not going in front of customer money. *Both ports refused connection
   from a developer machine during this review; see T0.*
2. **Ingest before decision.** The guide requires the record to exist first. Both calls happen in
   one worker invocation, sequentially; if ingest fails we do not call decision and the message
   retries.
3. **Idempotent.** SQS is at-least-once and unordered. Every ingest carries
   `Idempotency-Key: <object_id>:<state>`, and `replayed: true` tells us when the platform
   deduplicated. The worker compares `state_rank` so a replayed earlier state cannot overwrite a
   later one.
4. **Shadow first.** Phase 1 acts on nothing (§7).

### New files

| File | Purpose | LOC |
|---|---|---|
| `src/helpers/fraudEvents.ts` | `FraudEventPublisher` — envelope, hashing, masking, SQS push, never throws | ~120 |
| `src/services/core/fraudDetectionService.ts` | HTTP client for ingest + decision; maps our payloads to the agreed field names | ~200 |
| `src/types/fraud_types.ts` | Record shapes, state enum, decision shapes | ~90 |
| `src/jobs/fraud-event-worker.ts` | SQS worker: ingest → record-ingested | ~90 |
| `src/controllers/FraudController.ts` + route | `POST /callback/fraud-decision` — logs one structured line per scenario | ~65 |
| `tests/fraudEvents*.test.ts` | Tier-1 contract and safety tests (§8) | ~250 |

No changes to any existing worker other than the emit calls listed in §6.

### Infrastructure

- `FraudEventQueue` + `FraudEventDLQ`, following the `RecurringDebitSetupQueue` pattern.
- `FraudEventWorker` Lambda on that queue — copy `RecurringDebitSetupWorker`, `BatchSize: 10`,
  `ReportBatchItemFailures`.
- Globals: `FRAUD_EVENT_QUEUE`, `FRAUD_INGEST_URL`, `FRAUD_DECISION_URL`, `FRAUD_TENANT_ID`,
  `FRAUD_MODE` (`off` | `shadow` | `enforce`), `FRAUD_LATE_CALLBACK_SECONDS` (default 300).
- Credentials, if any are issued, go in the existing
  `transflow-online-external-service-credentials` SSM path.

`FRAUD_MODE=off` is the kill switch, checked in the publisher, so it stops all emission without
redeploying the business Lambdas.

`FRAUD_LATE_CALLBACK_SECONDS` must be an environment variable, not a constant — it is what makes the
late-callback rule testable in seconds instead of minutes (T11).

---

## 4. Data model

Three tables. `customers` and `wallets` exist because most money-movement fraud is only visible in
account and instrument history, not in a single transaction.

### 4.1 `transactions` — one record per **leg**

`object_id` is `reference_id`, already unique per leg — debit and credit share a `trans_id` but each
mints its own `reference_id`
([debit](../src/services/core/transactionService.ts#L409),
[credit](../src/services/payment_integrations/uniwalletService.ts#L266)).

| Field | Type | Enum / notes |
|---|---|---|
| `object_id` | string | = `reference_id` |
| `trans_id` | string | groups the two legs |
| `leg` | string | `debit`, `credit` |
| `state` | string | see below |
| `operation` | string | TRANSFER, AIRTIME, DATABUNDLE, BILLPAYMENT, INVOICE, VOUCHER, FUNDING, CHANGO, MOP, RPAY |
| `channel` | string | `app`, `ws`, `ussd`, `recurring`, `auto_retry`, `manual_retry`, `unknown` — **does not exist today, §6.3** |
| `amount` | float | amount actually sent to the provider |
| `original_amount` | float | pre-discount; null when none |
| `currency` | string | GHS, RWF, XAF, USD |
| `country_id` | string | GH, RW |
| `user_id` | string | → `customers.object_id` |
| `wallet_id` | string | → `wallets.object_id` |
| `source_msisdn` | string | funding wallet number |
| `destination_ref` | string | payout destination — **the fan-in pivot** |
| `destination_network` | string | credit rail |
| `funding_network` | string | debit rail |
| `merchant_product_id` | string | |
| `payment_ref` | string | customer-typed narration |
| `response_code` | string | nullable until callback |
| `response_message` | string | nullable until callback |
| `provider_transaction_id` | string | |
| `is_recurring` | bool | |
| `expected_callback` | bool | false for a clean provider rejection |
| `initiated_at` | timestamp | |
| `callback_received_at` | timestamp | nullable |
| `seconds_to_callback` | int | computed by us at emit time — drives the late-callback rule |
| `account_age_hours` | int | precomputed |
| `user_median_amount_90d` | float | precomputed |
| `user_txn_count_24h` | int | precomputed |
| `destination_first_seen_hours` | int | precomputed |

The precomputed fields are deliberate: because we cannot extend the platform, anything needing
history is cheaper to compute on our side and send as a scalar than to hope the rules engine can
derive it.

**`state` enum:**

| Value | Emitted in phase 1 |
|---|---|
| `initiated` · `awaiting_callback` · `callback_received` · `settled` · `failed` · `undetermined` · `blocked` | yes |
| `callback_overdue` · `service_not_initiated` · `ledger_missing` | **no — reserved** |

The three reserved values are declared now even though nothing emits them. A model change is a
cross-team request with a slow turnaround (§1), so reserving the values costs nothing today and
saves a round trip if absence-detection is ever scoped in. Flag them to the fraud team as reserved
so they are not expected in traffic.

`caption_field`: `object_id` · `semantic_type`: `entity`

### 4.2 `customers`

| Field | Type | Enum / notes |
|---|---|---|
| `object_id` | string | = `user_id` |
| `username_masked` | string | `23324***605` |
| `username_hash` | string | deterministic SHA-256, for cross-account matching |
| `auth_type` | string | `mobile`, `email` — **ownership rules must filter on `mobile`** (§5.6) |
| `display_name` | string | |
| `status` | string | active, dormant, suspended, pending |
| `kyc_tier` | string | nullable |
| `has_identity_number` | bool | |
| `country_id` | string | GH, RW |
| `registered_at` | timestamp | |
| `last_login_at` | timestamp | |
| `days_since_last_activity` | int | precomputed — dormancy signal |
| `distinct_devices` | int | precomputed |
| `auth_change_hours_ago` | int | precomputed — hours since credentials last changed; the takeover signal. Named to avoid the redaction key matcher, which silently ate `credentials_*` |

`caption_field`: `display_name` · `semantic_type`: `person`

### 4.3 `wallets`

| Field | Type | Enum / notes |
|---|---|---|
| `object_id` | string | = `wallet_id` |
| `user_id` | string | → `customers.object_id` |
| `wallet_type` | string | mobile, card, bank, ecedi, transflow |
| `wallet_number_masked` | string | |
| `wallet_number_hash` | string | **same hash across users = shared instrument** |
| `financial_entity_id` | string | |
| `is_third_party` | bool | wallet number ≠ account username. **Only meaningful when `auth_type = mobile`** |
| `is_verified` | bool | |
| `name_enquiry_name` | string | provider-returned holder name from [`nameEnquiry`](../src/services/core/walletService.ts#L577) — *currently fetched and discarded* |
| `account_holder_name` | string | from `user_info` |
| `name_match` | string | exact, partial, none, unknown — computed by us |
| `attached_at` | timestamp | |
| `minutes_since_signup` | int | precomputed |
| `third_party_wallet_count` | int | precomputed |
| `country_id` | string | GH, RW |

`caption_field`: `wallet_number_masked` · `semantic_type`: `entity`

### 4.4 Links and pivots

```
links:
  transaction_to_customer :  transactions.user_id   -> customers.object_id
  transaction_to_wallet   :  transactions.wallet_id -> wallets.object_id
  wallet_to_customer      :  wallets.user_id        -> customers.object_id

pivots:
  { base_table: transactions, field: destination_ref }     # cash-out fan-in
  { base_table: transactions, field: user_id }             # per-account velocity
  { base_table: transactions, field: trans_id }            # leg pairing
  { base_table: wallets,      field: wallet_number_hash }  # same instrument, many accounts
```

### 4.5 Data handling

- **Never send:** PINs, OTP values, passwords, tokens, card PANs/CVVs, raw provider payloads.
- Wallet numbers and usernames go as **masked + salted-hash pairs**; the salt lives in SSM.
- Full `destination_ref` is sent unmasked — a blocklist is useless against a hash we cannot read
  back, and it is already plaintext in `transaction_requests.account_ref`.
- The guide shows **no authentication on either service**. We cannot fix that, so we mitigate on our
  side: send nothing we would mind leaking, keep every identifier masked or hashed, and restrict VPC
  egress to that specific host and port.
- **Open: does sending customer names and masked MSISDNs to this tenant need compliance sign-off?**

---

## 5. Scenarios and rules

Five families covering the common money-movement fraud classes. **These are starting hypotheses, not
settled thresholds** — the weights are first guesses to be calibrated during shadow mode (§7).
Because rule changes are a slow cross-team loop, the aim is to send a broad set now and tune
weights, not to add rules piecemeal later.

Where a rule has a worked example from our own history it is cited. Those citations illustrate a
general pattern; they are not the reason the rule exists.

### 5.1 Family A — Destination / cash-out behaviour

```
Scenario name:        Suspicious destination concentration
Trigger object type:  transactions
When should it run:   Run for credit legs.
Score thresholds:     Review 50 · Block and review 80 · Decline 95

Rules:
1. Many senders, one destination     ≥3 distinct user_id → same destination_ref in 24h  → +50
2. Rapid repeat to destination       ≥3 credits to same destination_ref in 60 min       → +30
3. Destination newly seen            destination_first_seen_hours < 24                  → +20
4. Destination volume spike          24h volume ≥ 5× its own 30-day daily median        → +35
5. Unrelated senders                 senders share no device, no prior transfer history → +25
```

*Worked example:* one cash-out number received from ten distinct accounts over twelve days
(`docs/fraud/2026-09-09`, Addendum 1). Rule 1 alone would have flagged it at the third account.

### 5.2 Family B — Source instrument compromise

```
Scenario name:        Funding instrument risk
Trigger object type:  wallets
When should it run:   Run when a wallet is attached.
Score thresholds:     Review 40 · Block and review 70 · Decline 90

Rules:
1. Third-party wallet, no KYC         is_third_party AND NOT has_identity_number        → +40
2. Attached within 1h of signup       minutes_since_signup < 60                         → +30
3. Second+ third-party wallet         third_party_wallet_count ≥ 2                      → +50
4. Name mismatch                      name_match = none                                 → +35
5. Instrument seen on another account same wallet_number_hash elsewhere                 → +45
6. Verified without proof             is_verified AND name_match = unknown              → +15
```

*Calibration note:* rule 3 carries a **second** third-party wallet past review on its own, while a
single one needs corroboration. That split comes from a sweep of our own data which found
third-party attachment is normal, high-volume behaviour — roughly 400 in 45 days — so a rule firing
on *any* third-party wallet would be unusable. Expect this weight to move during shadow mode; it is
the most calibration-sensitive number in the set.

### 5.3 Family C — Account lifecycle

```
Scenario name:        Account lifecycle risk
Trigger object type:  transactions
When should it run:   Run when a debit leg is initiated.
Score thresholds:     Review 50 · Block and review 80 · Decline 95

Rules:
1. Day-zero account                  account_age_hours < 1                              → +30
2. New account                       account_age_hours < 24                             → +20
3. Dormant reactivation              days_since_last_activity > 90                      → +30
4. Movement after credential change  auth_change_hours_ago < 24                         → +40
5. High value on a thin account      amount > 1000 AND no KYC                           → +25
6. First transaction is high value   user_txn_count_24h = 1 AND amount > 1000           → +20
```

Rule 4 is the classic account-takeover shape, independent of anything in our own case files.

### 5.4 Family D — Behavioural and velocity

```
Scenario name:        Anomalous transaction behaviour
Trigger object type:  transactions
When should it run:   Run when a debit leg reaches a terminal state.
Score thresholds:     Review 50 · Block and review 75 · Decline 95

Rules:
1. Repeated failed debits            ≥3 failures on one wallet_id in 60 min             → +40
2. Descending amount sequence        each attempt lower than the last, ≥3 long          → +35
3. Success after ≥3 failures         the attempt that finally clears                    → +45
4. Probe then escalate               a <10 unit success followed by >500 within 1h      → +40
5. Far outside own baseline          amount ≥ 10× user_median_amount_90d                → +30
6. Burst velocity                    user_txn_count_24h ≥ 10                            → +25
7. Structuring                       ≥4 transactions just under a limit threshold in 24h → +35
```

*Worked example for rules 1–3:* an operator walking an amount down — 5000 → 2000 → 1550 → 1350 →
950 → 500 → 1 → 10 → 200 → 1000 — 55 debit attempts for 3 successes (`docs/fraud/2026-09-09`,
Addendum 3).

The test proposed in the brief — *"sending 5000 versus 4005"* — has the same shape, but the two are
not the same artifact and must not be treated as one. The rule exists because the pattern is real in
production; the test exists to prove the engine responds. **A UAT operator retrying a failed payment
at descending amounts produces this signature too**, which is why T4 passing says the rule fired, not
that the rule is right.

Rules 4–7 cover patterns we have not written up but which are standard in this fraud class and which
our data already supports.

### 5.5 Family E — Operational integrity

```
Scenario name:        Transaction resolution quality
Trigger object type:  transactions
When should it run:   Run when a leg reaches callback_received or undetermined.
Score thresholds:     Review 40 · Block and review 70 · Decline 100 (unreachable — never auto-blocks)

Rules:
1. Late callback                     seconds_to_callback > FRAUD_LATE_CALLBACK_SECONDS  → +30
2. Very late callback                seconds_to_callback > 900                          → +40
3. Late on the credit leg            leg = credit AND late                              → +20
4. Undetermined outcome              state = undetermined                               → +35
5. Repeated undetermined             ≥2 undetermined for one user_id in 24h             → +30
6. High value                        amount > 1000                                      → +10
```

Every rule here fires on a record we actually emit, with no dependency on the status checker. The
decline threshold is unreachable on purpose — this scenario reports, it never blocks.

### 5.6 Two things to get right in the definitions themselves

**Scope every ownership rule to `auth_type = 'mobile'`.** For an email-auth account the username is
an email address, so it never equals a wallet number and *every* wallet flags as third-party. We hit
exactly this in our own analysis SQL and it put two internal test accounts at the top of a fraud
ranking. Put the filter in the field definition, not in a comment.

**Do not ask for name clustering.** We tried it; common Ghanaian given names returned ~60 accounts,
overwhelmingly real customers. It is noise, and it costs a rule slot we cannot easily reclaim.

---

## 6. Emit points

Twelve call sites. Each is one `await publisher.publish(...)` inside an existing try/catch, placed
**after** the durable write it describes. None of them touch the transaction status checker.

### 6.1 Transaction lifecycle

| # | Where | State |
|---|---|---|
| 1 | [`processTransaction`](../src/services/payment_integrations/uniwalletService.ts#L46), after `writeTransRequest` | `debit / initiated` |
| 2 | same, after `armNoCallbackWatchdog()` | `debit / awaiting_callback`, `expected_callback=true` |
| 3 | same, catch branch with `noCallbackExpected` | `debit / failed`, `expected_callback=false` |
| 4 | [`debitCallbackProcess`](../src/services/payment_integrations/uniwalletService.ts#L740), after `writeTransResponse` | `debit / callback_received` + `seconds_to_callback` |
| 5 | [`fulfillService`](../src/services/payment_integrations/uniwalletService.ts#L260), after `writeTransactionEntry` | `credit / initiated` → `awaiting_callback` |
| 6 | [`creditCallbackProcess`](../src/services/payment_integrations/uniwalletService.ts#L943) | `credit / callback_received` + `seconds_to_callback` |
| 7 | [`db-writer-worker`](../src/jobs/db-writer-worker.ts) terminal write | `settled` / `failed` / `undetermined` |
| 8 | [`isBlockedTransferDestination`](../src/jobs/transaction-processing-worker.ts#L36) | `blocked` |

Points 2 and 5 sit beside the existing `SchedulerHelper` arming calls but do not modify them — the
current watchdog keeps running exactly as it does now (§2).

### 6.2 Wallet and customer

| # | Where | Emitted |
|---|---|---|
| 9 | [`WalletService.addWallet`](../src/services/core/walletService.ts#L496), after commit | `wallets` upsert incl. `is_third_party`, `name_match`, `third_party_wallet_count` |
| 10 | same, `nameEnquiry` result | populates `name_enquiry_name` — *today this response is logged and thrown away* |
| 11 | [`UsersService.addUser`](../src/services/core/usersService.ts#L507), after commit | `customers` upsert |
| 12 | `UsersService.updateUser` on `status` change | `customers` status change |

### 6.3 The `channel` field does not exist yet

There is no origination channel on a transaction. The only marker is `callingParty: 'ws'`
([TransactionsWsController.ts:175](../src/controllers/TransactionsWsController.ts#L175)) and it is
never persisted. Set `meta.channel` in
[`buildTransactionPayload`](../src/services/core/transactionService.ts#L391) from `callingParty`,
`is_recurring` and `processMode`, then carry it onto the fraud record.

**On USSD:** USSD transactions do not originate in this repository. The only trace is the
`ussd_linked_accounts` model, which links an MSISDN to a product — there is no USSD transaction path
in `src/`. Either USSD runs through a separate service needing its own integration, or it reaches us
through a path not yet identified. **Confirm before promising USSD coverage** — we cannot tag what we
do not originate.

---

## 7. Enforcement

| Phase | Mode | Behaviour | Exit criteria |
|---|---|---|---|
| **1** | `shadow` | Emit, receive decisions, log them, act on **nothing**. | ≥2 weeks production traffic; measured false-positive rate per family; weights tuned. |
| **2** | `enforce` | Block on `decline` / `block_and_review` **at the credit leg only**. | Acceptable precision on families A and D. |
| **3** | — | Consider a synchronous pre-debit check. | Only with a hard ≤800ms timeout, fail-open, and measured latency impact. Not scoped here. |

Shadow mode is not a formality. Because we cannot iterate rules quickly, phase 1 is where the weight
calibration happens, and it needs enough traffic to produce a real false-positive rate per family.

### Why the credit leg, not pre-debit

- **Pre-debit** — [`initiateCommonTransaction`](../src/services/core/transactionService.ts#L130),
  before `provider.processTransaction()`. The only place to stop money *before* it leaves the
  customer, but it demands a synchronous call to an unproven service on the checkout path.
- **Credit leg** — [`isBlockedTransferDestination`](../src/jobs/transaction-processing-worker.ts#L36).
  The customer is already debited, so a block means a reversal — but the money does not reach the
  fraudster, and funds stopped before the credit leg are recoverable from float where funds already
  credited out are not.

Phase 2 reuses the existing block machinery wholesale: same `recordBlockedTransfer` audit, same
`transaction_destination_restricted` socket response, same **fail-open on lookup error**. A fraud
service outage must never block legitimate transfers.

---

## 8. Testing

Every test has a stated expected outcome, so each is pass/fail rather than a judgement call. Step-by-step
run instructions for each are in §14. **No test below depends on the transaction status checker, and
none requires waiting or provider cooperation.**

**What these tests establish, and what they do not.** They prove the pipe works: records arrive
intact, scenarios evaluate, decisions come back, and nothing we added can affect a transaction. They
prove nothing about whether a rule is *correct*. UAT traffic is our own staff on test accounts —
every account is day-zero with no KYC, colleagues send to the same handful of destinations, and
amounts get retried downwards because someone is hunting for one that clears. Several rules will
fire on that legitimately.

So **no false-positive rate measured in UAT means anything**, and no threshold should be tuned to
make a test pass. Calibration needs production shadow traffic, which is what phase 1 (§7) is for.
The `UAT smoke test` scenario in
[fraud-scenarios-and-rules.md](fraud-scenarios-and-rules.md) exists for exactly this reason: a
trivial, deterministic trigger keyed on `payment_ref` containing "FRAUDTEST", so responsiveness can
be proven without leaning on a real fraud rule at all.

### Tier 1 — Unit (jest, no network)

Follows the prototype-spy pattern in
[tests/transactionProcessingWorker.test.ts](../tests/transactionProcessingWorker.test.ts).

| Test | Asserts |
|---|---|
| Publisher swallows SQS failure | `pushDataToQueue` rejects → `publish()` resolves, business path untouched |
| `FRAUD_MODE=off` | no SQS call at all |
| Field-name contract | mapper output keys match the agreed model **exactly** — the guide warns names must match, and a silent rename is the likeliest way this rots |
| Enum contract | every enum field emits only declared values, and never a reserved one |
| Redaction | no `pin`, `otp`, `password`, `token`, `access_token` survives mapping — assert on a payload seeded with all five |
| Worker ordering | ingest resolves before `record-ingested` is called |
| Worker failure | ingest rejects → `record-ingested` never called → message retried |
| Monotonic state | a replayed earlier state does not overwrite a later one |
| `expected_callback=false` | a clean rejection emits `failed`, never `awaiting_callback` |
| `seconds_to_callback` | computed from `initiated_at` → `callback_received_at`, not from wall clock at worker time |

### Tier 2 — Local end-to-end (no vendor dependency)

The harness exists: [local-server.js:325](../local-server.js#L325) patches `sqs.pushDataToQueue` to
run workers in-process, and
[local-callback-simulator.js](../local-websocket-testing/local-callback-simulator.js) fires the
callbacks production would send. One addition: `local-fraud-worker.js`, intercepting
`FRAUD_EVENT_QUEUE` pushes the same way the transaction and db-writer queues already are, pointed at
a stub HTTP server that records every ingest so the exact sequence can be asserted offline.

`LOCAL_DEBIT_CALLBACK_DELAY_MS` already exists and drives T11 without any new flags.

### Tier 3 — Measurable UAT scenarios

| # | Scenario | How to run | Pass condition |
|---|---|---|---|
| **T1** | **Does it actually store what we send?** | Run one transfer, then `GET /v1/tenants/{id}/records/transactions/{object_id}` for each leg | Every field we sent is stored and matches — types, enums, timestamps. *The most literal answer to "will their system track it"* |
| **T2** | Leg tracking and pairing | one normal UAT transfer | exactly 4 records (debit init/cb, credit init/cb), all terminal, both legs linked by `trans_id`, searchable by it |
| **T3** | Failed debit classification | debit above balance → response `529` | `state=failed`, `expected_callback=false`, `response_code=529` stored |
| **T4** | **Amount walk-down** | one account → one destination: 5000 → 4005 → 3000 → 1000 until one clears | family D crosses review at the 3rd failure; `review` on the success. Record which attempt tipped it — that number is the calibration |
| **T5** | **Destination fan-in** | 3 UAT accounts → one destination inside 10 min | fires on the 3rd; decision `review`; the `destination_ref` pivot returns all three via record search |
| **T6** | Probe then escalate | small success, then a large amount within the hour | family D rule 4 hits |
| **T7** | Outside own baseline | established test account sends 10× its usual amount | family D rule 5 hits |
| **T8** | Third-party wallet attach | test account attaches a wallet it does not own | family B ≥70 → `block_and_review` |
| **T9** | Second third-party wallet | same account attaches a second | score jumps by the rule-3 weight — confirms the calibration |
| **T10** | Day-zero velocity | fresh account transacts within 15 min | family C → `review` |
| **T11** | **Late callback** | set `FRAUD_LATE_CALLBACK_SECONDS=10`; locally set `LOCAL_DEBIT_CALLBACK_DELAY_MS=15000` | `seconds_to_callback≈15`, family E rule 1 hits, score 30. Proves a time-based rule works, in 15 seconds |
| **T12** | Undetermined outcome | force a non-explicit failure response code | `state=undetermined`, family E rule 4 hits |
| **T13** | **Fraud service unavailable** | point `FRAUD_INGEST_URL` at a black hole | **transactions complete normally.** Messages retry, reach the DLQ, zero customer impact, no latency change. *The most important test here* |
| **T14** | Idempotency | re-POST an identical `object_id` + state | `replayed: true`; the worker's state-rank guard logs a skip rather than re-ingesting |
| **T15** | Volume | 500 transactions in 10 min | no queue backlog, no added transaction latency, all 2000 legs present and retrievable |

T1, T2 and T5 use the guide's record read and search endpoints, which means "did it track it" is
answered by querying the platform directly rather than by inference from a decision.

### T0 — Pre-flight, before any of the above

From a Lambda in the application VPC, confirm `54.246.247.31:8087` and `:8088` are reachable. Both
refused connection from a developer machine during this review. Every existing outbound integration
uses HTTPS/443 to `apis.itcsrvc.com`, so **egress on 8087/8088 to a bare IP has never been exercised
from these subnets** and the security group may not permit it. A throwaway Lambda answers this in an
hour and de-risks everything downstream.

---

## 9. Open questions

1. **Is 8087/8088 reachable from our VPC?** Blocks everything. *DevOps, or run T0.*
2. **What is our tenant ID?** *Fraud team.*
3. **Does USSD run through this API at all?** (§6.3) *Supervisor.*
4. **Can we send masked MSISDNs and customer names to this tenant?** *Compliance.*
5. **Async callback or polling for decisions?** The guide offers `callback_url`; preference is the
   callback, which needs a public route and, ideally, a shared secret.
6. **Who watches the decisions?** A decision nobody reads does not exist. Phase 1 should land in a
   dashboard or support queue on day one, not on the day enforcement is switched on.
7. **Is the absent-callback alert wanted as a product feature?** (§2) Out of scope here, and the
   existing watchdog still covers it. Raise it as its own piece of work if the answer is yes.

Noted but not actionable, since we cannot change the platform: there is **no authentication on
either service** in the guide. Mitigations are on our side (§4.5); still worth raising with the
fraud team as a finding.

---

## 10. Sequence

| Step | Work | Depends on |
|---|---|---|
| 0 | T0 reachability probe | — |
| 1 | Send [`fraud-data-model.json`](fraud-data-model.json) + [`fraud-scenarios-and-rules.md`](fraud-scenarios-and-rules.md) to the fraud team; get tenant ID | — |
| 2 | `fraudEvents.ts`, `fraud_types.ts`, `fraudDetectionService.ts` + tier-1 tests | — (parallel with 1) |
| 3 | Infra: queue, DLQ, worker, env vars, `FRAUD_MODE=off` | 0 |
| 4 | Emit points 1–8 (transaction lifecycle) | 2, 3 |
| 5 | Local harness addition + tier-2 tests | 4 |
| 6 | Deploy UAT, `FRAUD_MODE=shadow`, run **T1–T15** | 4, 5 |
| 7 | Emit points 9–12 (wallet, customer) + `channel` field | 6 |
| 8 | Production shadow ≥2 weeks; tune weights | 7 |
| 9 | Phase 2 enforcement at the credit leg | 8 |

Step 6 is the demonstrable milestone: the full test matrix, run end to end, with nothing in
production behaviour changed.

---

## 11. Risks

| Risk | Mitigation |
|---|---|
| Fraud service latency or outage bleeds into transactions | Queue + worker; nothing synchronous in phase 1; `FRAUD_MODE=off` kill switch; T13 proves it |
| Field-name drift silently breaks ingest | Tier-1 contract test on exact key names; alert on `validation_failed` responses |
| Rule weights are wrong and we cannot iterate quickly | Broad set sent up front; shadow mode is where calibration happens; weights chosen to be tunable without restructuring rules |
| False positives block real customers | Shadow ≥2 weeks; enforce on `decline`/`block_and_review` only; fail-open |
| Over-fitting to one observed fraud pattern | Five families spanning destination, instrument, lifecycle, behaviour and operations; written-up cases cited as examples, not as the basis |
| Ownership rules misfire on email accounts | `auth_type = 'mobile'` filter baked into the field definition, not left as a note |
| Absence of callback goes unnoticed | Out of scope by decision (§2); the existing `NO_CALLBACK_AFTER_PAYMENT_INITIATION` watchdog is untouched and still covers it |

---

## 12. Implementation shape — what gets created, and what calls what

### 12.1 The layering rule

```
 business services ──► src/helpers/fraudEvents.ts ──► SQS FRAUD_EVENT_QUEUE
   (in-VPC Lambdas)      FraudEventPublisher                    │
                                                                ▼
                                        src/jobs/fraud-event-worker.ts
                                                                │
                                                                ▼
                                src/services/core/fraudDetectionService.ts
                                                                │
                                                                ▼
                                                       vendor HTTP :8087/:8088
```

**Business code never imports `fraudDetectionService`.** It only ever imports the publisher helper.
The vendor HTTP client lives entirely on the worker side of the queue, so no Lambda on the payment
path can block on, retry against, or throw because of the fraud service. This is the property that
makes T13 pass, and it is worth a unit test that asserts the import graph rather than relying on
review.

The split mirrors what the repo already does: `SchedulerHelper`
([helpers/scheduler.ts](../src/helpers/scheduler.ts)) is a thin helper over `sqs` that business code
calls, while the real consumer lives elsewhere. `FraudEventPublisher` is the same shape.

### 12.2 Files created

| File | Layer | Responsibility |
|---|---|---|
| `src/helpers/fraudEvents.ts` | helper | `FraudEventPublisher.publish()` — builds the envelope, masks/hashes identifiers, checks `FRAUD_MODE`, pushes to SQS. **Never throws.** |
| `src/types/fraud_types.ts` | types | Record shapes, `FraudState` enum, decision payload shapes |
| `src/services/core/fraudDetectionService.ts` | core service | Extends `BaseService`. `ingest()`, `triggerDecision()`, and the field mappers. The only file that knows the vendor wire format. |
| `src/jobs/fraud-event-worker.ts` | job | SQS worker — ingest, then record-ingested. Modelled on [`recurring-debit-setup-worker.ts`](../src/jobs/recurring-debit-setup-worker.ts) |
| `src/controllers/FraudController.ts` | controller | `decisionCallback` — logs one structured line per scenario |
| `src/router/http/fraud.ts` | router | `POST /callback/fraud-decision`, registered in [`routes.ts`](../src/router/routes.ts) |
| `tests/fraudEvents.test.ts`, `tests/fraudEventWorker.test.ts` | tests | Tier 1 (§8) |

### 12.3 Files modified — the full list

| File | Change | Emits |
|---|---|---|
| [`uniwalletService.ts`](../src/services/payment_integrations/uniwalletService.ts) | 6 publish calls | 1–6 |
| [`db-writer-worker.ts`](../src/jobs/db-writer-worker.ts) | 1 publish call | 7 |
| [`transaction-processing-worker.ts`](../src/jobs/transaction-processing-worker.ts) | 1 publish call in `isBlockedTransferDestination` | 8 |
| [`walletService.ts`](../src/services/core/walletService.ts) | 2 publish calls; retain the `nameEnquiry` response instead of discarding it | 9–10 |
| [`usersService.ts`](../src/services/core/usersService.ts) | 2 publish calls | 11–12 |
| [`transactionService.ts`](../src/services/core/transactionService.ts) | set `meta.channel` in `buildTransactionPayload` | — |
| [`routes.ts`](../src/router/routes.ts) | register `fraudRoutes` | — |
| [`template.yaml`](../template.yaml) | queues, worker, parameters, env vars | — |

Nothing else. In particular: **no change to `check-transaction-status-worker.ts`,
`checkTransactionStatusService.ts`, `retryService.ts`, or `transactionRecoveryService.ts`.**

---

## 13. UAT deployment

### 13.1 Where each setting lives, and why

| Setting | Lives in | Reason |
|---|---|---|
| `FraudMode` | **template parameter**, default `shadow` | Emission is on by default so a UAT deploy needs no override. Shadow acts on nothing, so this is safe to default; `enforce` never is. Pin prod to `off` with a deploy override if you want it dark until the tenant exists — same artifact either way, no code divergence |
| `FraudLateCallbackSeconds` | **template parameter**, default `300` | UAT deploys `10` so T11 runs in seconds |
| `FRAUD_EVENT_QUEUE` | template (queue URL) | standard |
| `fraudDetection.ingestUrl`, `.decisionUrl`, `.tenantId` | **SSM**, under the existing `/transflow-online-external-service-credentials/` path | Already per-environment and already loaded by [`configuration.loadConfigData`](../src/config/config.ts#L357) into `configData.transflowOnlineExternalService`. Same treatment as `uniwallet`, `billPayment`, `directDebit`. **Changing tenant or host needs no deploy.** |

Adding a `fraudDetection` block to the SSM blob is the same operation as any other integration's
credentials — no new mechanism.

### 13.2 Deploy order

1. **Run T0** — throwaway Lambda in the UAT VPC, confirm `54.246.247.31:8087` and `:8088` answer.
   Resolve §13.3 before going further.
2. **Add the `fraudDetection` block** to the UAT `transflow-online-external-service-credentials`
   SSM parameter (ingest URL, decision URL, tenant ID).
3. **Deploy the stack** to UAT with `FraudLateCallbackSeconds=10`, `EnvironmentName=uat`,
   `LogLevel=DEBUG` and a real `FraudHashSalt`. `FraudMode` already defaults to `shadow`.
   **There is no database migration — nothing in this integration writes to our DB.**
4. **Smoke check** — run one transfer; confirm the
   `/aws/lambda/transflow-online-fraud-event-worker` log group exists and shows an ingest, and that
   the DLQ is empty.
5. **Run the §14 runbook.**

Production stays `FraudMode=off` throughout. Turning it on later is a parameter change, not a code
change.

### 13.3 One decision to make before deploying: VPC placement

All Lambdas inherit `VpcConfig` from `Globals`
([template.yaml:38](../template.yaml#L38)). Two options for the fraud worker:

| | Placement | Needs | Trade-off |
|---|---|---|---|
| **Default** | In the VPC, like every other worker | Security-group egress to `54.246.247.31:8087-8088` | Consistent; the worker can query the DB for the history-derived fields (`user_median_amount_90d`, `destination_first_seen_hours`, …) |
| **Fallback** | `VpcConfig: !Ref AWS::NoValue` on this worker only | nothing — plain internet egress | Sidesteps the egress question entirely, but the worker loses DB and Redis, so the history-derived fields must be dropped from phase 1 or cached in Redis by a separate path |

**Recommendation: default (in-VPC), and open the egress.** Every existing outbound integration is
HTTPS/443 to `apis.itcsrvc.com`, so a bare IP on 8087/8088 has never been exercised from these
subnets and may simply not be permitted. If DevOps cannot open it quickly, take the fallback and
defer the history fields — that still leaves 13 of the 15 UAT tests runnable.

The publisher side is unaffected either way: business Lambdas only push to SQS, which already works
from inside the VPC for every existing queue.

---

## 14. Test runbook

### 14.1 The three places to look

| | Where | Answers |
|---|---|---|
| **A** | `/aws/lambda/transflow-online-fraud-event-worker` (profile `staging-v2`, `eu-west-1`) | What did we send, and what did the platform reply? |
| **B** | `GET http://<ingest>/v1/tenants/<tenant>/records/transactions/<object_id>` | Did they actually store it? |
| **C** | `==FRAUD DECISION==` lines in `/aws/lambda/transflow-online-api-generic-route` | What did they decide, and on which rules? One line per scenario carrying `scenario_id`, `outcome`, `score` and `rules_hit` — groupable in CloudWatch Insights |

B is the one that answers the original question — *"will their system track it"* — directly, rather
than by inference.

### 14.2 Per-test steps

All UAT requests go through the existing Postman collection
([docs/postman/transflow-online-api.postman_collection.json](postman/transflow-online-api.postman_collection.json))
with the UAT environment
([docs/transflow-online-api-uat.postman_environment.json](transflow-online-api-uat.postman_environment.json)).
The transfer endpoint is `POST /checkout-debit`
([transactions.ts](../src/router/http/transactions.ts)); wallet attach is `POST /wallets`.

| # | Run | Verify |
|---|---|---|
| **T1** | One `POST /checkout-debit`. Take `reference_id` from the worker log. | **B** — fetch the record; diff every field against what the worker logged as sent. Types, enums, timestamps. |
| **T2** | Same transfer, let both legs complete. | **B** — `GET /records/transactions/search?field=trans_id&value=<trans_id>` returns **4** records: debit init/cb, credit init/cb. |
| **T3** | `POST /checkout-debit` from a UAT wallet with insufficient balance (expect `529`). | **A/B** — `state=failed`, `expected_callback=false`, `response_code=529`. |
| **T4** | Four sequential `POST /checkout-debit` — 5000, 4005, 3000, 1000 — same `account_ref`, same account, inside an hour. | **C** — family D crosses review. **Record which attempt tipped it**; that number is the calibration input for shadow mode. |
| **T5** | Three different UAT test users → the same `account_ref`, inside 10 minutes. | **C** — fires on the 3rd, decision `review`. **B** — `search?field=destination_ref` returns all three. |
| **T6** | One transfer of 5, then one of 800, same account, inside the hour. | **C** — family D rule 4 (probe then escalate). |
| **T7** | An established UAT account with transfer history sends 10× its usual amount. | **C** — family D rule 5. *Needs a seeded account; the history fields must be live (§13.3).* |
| **T8** | `POST /wallets` attaching a mobile number that is **not** the account's own username. | **C** — family B ≥70 → `block_and_review`. |
| **T9** | Same account attaches a **second** third-party wallet. | **C** — score jumps by the rule-3 weight. Confirms the "two or more is the signal" calibration. |
| **T10** | Fresh sign-up, then `POST /checkout-debit` within 15 minutes. | **C** — family C → `review`. |
| **T11** | Deploy UAT with `FraudLateCallbackSeconds=2` so *every* callback is late. Run one transfer. Locally instead: `LOCAL_DEBIT_CALLBACK_DELAY_MS=15000` with the threshold at 10. | **C** — family E rule 1 hits, `seconds_to_callback` matches observed latency. Put the parameter back afterwards. |
| **T12** | Force a response code that is **not** in `EXPLICIT_FAILURES`. | **A/B** — `state=undetermined`; **C** — family E rule 4. |
| **T13** | Point `fraudDetection.ingestUrl` in UAT SSM at an unroutable host. Run 3 transfers. | Transfers **complete normally**, same latency. Worker retries, messages land in the DLQ, no customer-visible change. Restore the URL, redrive the DLQ, confirm the records arrive late but intact. |
| **T14** | Redrive one already-processed SQS message, or re-POST the same `object_id` + state. | **A** — `replayed: true`, and a "Skipping out-of-order fraud event" line for a replayed earlier state. |
| **T15** | Script 500 `POST /checkout-debit` over 10 minutes. | Queue depth stays near zero; `/checkout-debit` p95 unchanged vs a baseline run with `FraudMode=off`; **B** — all 2000 leg records retrievable. |

### 14.3 What can be run locally first

T1–T4, T6, T8, T9, T11, T12 and T14 all run against `npm run dev:watch` with the local harness —
[local-server.js](../local-server.js) already simulates the provider callbacks and runs the workers
in-process, and `local-fraud-worker.js` (§8, tier 2) points the ingest at a stub that records
everything. Running them locally first means UAT is a confirmation pass, not a debugging session.

T5, T7, T10, T13 and T15 need UAT: multiple real accounts, seeded history, real SSM, or real
queue behaviour.

---

## 15. Implementation status

Implemented on branch `fraud-detector`. Typecheck clean, 43 fraud tests passing, no existing test
affected.

`FraudMode` defaults to **`shadow`**: emission is on the moment a stack is deployed from this
code. Shadow records decisions and acts on nothing — no transaction is blocked, delayed or altered —
so the blast radius of getting this wrong is CloudWatch noise, not customer impact. Until a
`fraudDetection` block exists in a stack's SSM parameter, that stack's worker cannot resolve the
endpoints and every event lands in `transflow-online-fraud-event-dlq`; transactions are unaffected
either way. Pin a stack to `FraudMode=off` at deploy time if you want it emitting nothing.

### Built

| Area | Files |
|---|---|
| Publisher (the only module business code imports) | `src/helpers/fraudEvents.ts` |
| Record shapes and state enum | `src/types/fraud_types.ts` |
| Vendor HTTP client + decision persistence | `src/services/core/fraudDetectionService.ts` |
| Queue worker | `src/jobs/fraud-event-worker.ts` |
| Decision callback | `src/controllers/FraudController.ts`, `src/router/http/fraud.ts` |
| Infrastructure | `template.yaml` — `FraudEventQueue`, `FraudEventDLQ`, `FraudEventWorker`, `FraudMode`/`FraudLateCallbackSeconds`/`FraudHashSalt` parameters |
| Tests | `tests/fraudEvents.test.ts` (32), `tests/fraudEventWorker.test.ts` (13) |
| Local harness | `local-websocket-testing/local-fraud-worker.js`, plus `LOCAL_SUPPRESS_DEBIT_CALLBACK` / `LOCAL_SUPPRESS_CREDIT_CALLBACK` |

All 12 emit points are in place. Changes to existing files are purely additive — 348 inserted
lines, nothing deleted — and the transaction status checker is untouched, as
`tests/fraudEvents.test.ts` asserts.

### Decisions taken during implementation

**Publishes are awaited, with a 2s ceiling.** A Lambda freezes the moment its handler returns, so an
unawaited SQS push is simply lost. Awaiting costs roughly 20–60ms per event on a debit that already
takes seconds. `FRAUD_PUBLISH_TIMEOUT_MS` (default 2000) bounds the case where SQS stops responding,
so a degraded queue can slow a transaction by at most that much rather than by the SDK's own retry
budget. T15 measures the real cost against a `FRAUD_MODE=off` baseline.

**A 4xx from the platform is permanent, not retryable.** Redelivering an identical payload produces
an identical rejection, so `FraudValidationError` is acknowledged and logged loudly rather than
burning three attempts and a DLQ slot. That log line is the signal that our model and theirs have
drifted.

**`shouldSend` initialises itself** rather than relying on `loadEndpoints()` having run first, so
reordering `processEvent` cannot silently turn the monotonic guard into a no-op.

**`credentials_changed_hours_ago` was renamed to `auth_change_hours_ago`.** The original name
contained `credential`, which the publisher's redaction matcher strips — the field was being
silently removed from every customer record before it left. Caught by the
"redaction does not eat a contracted field" test, which now guards the whole contract permanently.

**Decisions are logged, not stored.** There is no `fraud_decisions` table and no migration. Shadow
mode acts on nothing, so one structured line per scenario — `scenario_id`, `outcome`, `score`,
`rules_hit` — answers every question the test matrix asks, and CloudWatch Insights aggregates it.
A table only earns its keep once enforcement needs to look a decision up, or once calibration wants
aggregates beyond log retention. Adding it then is small and self-contained; nothing here has to
move to accommodate it.

### Before deploying to UAT

1. **T0** — reachability of `54.246.247.31:8087/:8088` from the application VPC, and the §13.3
   VPC-placement decision.
2. Add a `fraudDetection` block — `ingestUrl`, `decisionUrl`, `tenantId` — to the UAT
   `transflow-online-external-service-credentials` SSM parameter.
3. Deploy with `FraudHashSalt=<stable secret>` and `FraudLateCallbackSeconds=10`. `FraudMode`
   is already `shadow`; pass `FraudMode=off` to any stack that should stay dark.
4. For local runs, add `FRAUD_MODE=shadow`, `FRAUD_EVENT_QUEUE=local`, `FRAUD_HASH_SALT=<anything>`
   to `src/config/.env.local`. With no `FRAUD_INGEST_URL` set, the local worker records every
   envelope to `local-websocket-testing/logs/fraud-events.jsonl` and sends nothing off the machine.
