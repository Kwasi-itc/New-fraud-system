# Case intake contract (v1)

All APIs share one PostgreSQL database. Decision, screening, and case tables retain their service owners. Case intake never trusts caller-supplied actor headers or modifies another service's records.

## Workflow actions

`POST /internal/v1/workflow-actions`, authenticated by the case integration bearer token. The token's configured tenant allowlist must contain `tenant_id`.

```json
{
  "workflow_execution_id": "11111111-1111-4111-8111-111111111111",
  "tenant_id": "22222222-2222-4222-8222-222222222222",
  "decision_id": "33333333-3333-4333-8333-333333333333",
  "scenario_id": "44444444-4444-4444-8444-444444444444",
  "action_type": "add_to_case_if_possible",
  "action_config": {
    "inbox_id": "55555555-5555-4555-8555-555555555555",
    "name": "Payment investigation",
    "tag_ids": [],
    "any_inbox": false
  }
}
```

| Action | Behavior |
| --- | --- |
| `create_case` | Create a new pending decision case for this execution |
| `add_to_case` | Reuse an eligible open case, or return 404 without writing |
| `add_to_case_if_possible` | Reuse an eligible open case; otherwise create one |
| `add_tag`, `emit_event` | Rejected by case intake. The decision dispatcher requires an explicit HTTP(S) URL for these external actions and does not send the internal service credential to it |

The decision engine validates the same action/configuration rules when authoring and dispatching. There is currently no workflow-authoring UI in the new frontend; its decision-detail view only displays execution records.

`scenario_id`, if present, must match the authoritative decision. Optional `object_type`/`object_id` assertions must also match. Case-manager derives grouping from the owning decision's `(object_type, object_id)` and ignores supplied `pivot_value`; the present decision schema has no authoritative configurable pivot field. The persisted pivot string is `object_type:object_id`.

Scope is the configured inbox unless `any_inbox=true`. Eligible cases belong to the same tenant, are not closed, and have an active inbox. Newest `created_at`, then greatest case ID, wins deterministically. A transaction-scoped advisory lock covers the tenant/object across all inboxes, so scoped and any-inbox operations participate in the same synchronization. Unrelated objects proceed independently. The chosen case is locked before attachment; concurrent close/move operations are revalidated.

The title is a literal `name`, then literal `title`, then `Workflow case`. `title_template` AST and per-case dispatcher `url` are explicitly rejected. Marble's create versus reuse and automated-pending behavior are preserved; custom pivot evaluation and AST title templates are not silently emulated.

Receipts are keyed by `(tenant_id, source='workflow', workflow_execution_id)`. A normalized typed request is hashed, including action configuration and tag order. An identical retry returns the recorded case without additional links/events/tags. A conflicting use of the same ID returns 409. Receipts commit with all case writes; failure rolls everything back. Response is `200 {"case": ...}`; retries may return the case's current state. Retain receipts for as long as producer executions can be replayed.

## Screening reviews

`POST /v1/screening-events/reviewed`, authenticated by the same integration boundary:

```json
{
  "event_id": "66666666-6666-4666-8666-666666666666",
  "tenant_id": "22222222-2222-4222-8222-222222222222",
  "screening_id": "77777777-7777-4777-8777-777777777777",
  "decision_id": "33333333-3333-4333-8333-333333333333",
  "match_id": "provider-match-id",
  "status": "no_hit"
}
```

Screening creates an event UUID and immutable payload in `screening.case_event_outbox` in the same transaction as the review. Case-manager receipts deduplicate that event ID, reject conflicting payloads, and serialize different events for the same screening. It validates screening/match ownership and decision identity through the shared database. Routing first uses an indexed existing screening link, then an indexed decision link, including snoozed cases. Without either link, exactly one configured review inbox is required; missing/ambiguous routing fails explicitly so delivery can be retried after configuration is repaired.

Each match has its own case-screening link. Audit history records the delivered review status, while the link reflects the current authoritative match status, preventing delayed retries from regressing the displayed status. `reviewer_id` is not accepted as verified human identity. Success returns 204 only after the receipt and mutation commit.

## Durable delivery

The default `screening-case-delivery-worker` runs real callback delivery independently of provider screening jobs. It leases one due outbox row at a time using `FOR UPDATE SKIP LOCKED`, commits the claim before HTTP, and fences completion by attempt number. The 60-second lease exceeds the 30-second delivery deadline. Crashes leave recoverable expired leases. HTTP failures, redirects, missing URLs/tokens, and unsupported responses never count as successful delivery.

Retries retain the event ID and payload, back off exponentially (capped), and stop in `failed` after 20 completed failed attempts. `status`, `attempts`, `available_at`, `lease_until`, `last_error`, and `delivered_at` provide operational evidence. A lost acknowledgement can redeliver a committed callback; the receiver's receipt prevents duplicate effects. After fixing the cause, operators can requeue a failed row by setting `status='pending'`, `attempts=0`, `available_at=now()`, `lease_until=NULL`, without changing its ID/payload. Never rewrite an event to bypass a receiver's conflict response.

`/v1/screening-events/evidence-uploaded` returns **501** until evidence intake is implemented. File creation still atomically queues the notification; it remains observable and eventually failed rather than being silently lost or falsely marked delivered. Existing authenticated manual attachment using an authoritative `source_file_id` remains available.

## Migration and rollback

Apply screening 000004 and case-manager 000004 after the existing owner migrations. Case-manager 000004 changes screening-link uniqueness from one row per screening to one row per match and adds receipts. A downgrade refuses to merge multiple matches. Receipt history and screening outbox rows survive downgrade/re-upgrade; no destructive automatic repair is performed. Apply changes while the old consumers are quiesced, since old code targets the previous screening-link conflict key and older callbacks omit `event_id`.

## Verification

Case-manager PostgreSQL tests cover simultaneous identical retries, conflicting IDs, grouping/closed-case behavior, receipt rollback, and match-preserving callback deduplication. Screening PostgreSQL tests cover transactional enqueue rollback, failed delivery/recovery, stable IDs, lease expiry, and stale-worker fencing. Decision dispatcher contract tests verify case-specific authentication and separation of external actions. Disposable integration databases are never the configured application database.
