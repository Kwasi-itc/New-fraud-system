# Rule Evidence Implementation Plan

Date: July 28, 2026

## Goal

Add Marble-style per-rule evaluation evidence to the new decision-engine service so decision details can explain why a rule hit. The target outcome is support for explanations such as `500 > 300` on the decision detail page.

## Scope

- Backend decision-engine service persistence and API changes
- Frontend decision detail rendering changes
- Tests and documentation for the new response shape

## Implementation Plan

1. Define the target response contract for rule-hit evidence.
   Decide the exact shape the frontend should consume for each rule execution, including result, outcome, optional error details, and structured evaluation data.

2. Trace the evaluation path in the new service.
   Follow rule evaluation from execution through decision assembly to identify where evaluation details need to be captured and preserved.

3. Extend the rule execution domain model.
   Add fields for rule result and evaluation details to the `decision.RuleExecution` model in the new service.

4. Add persistence for rule evaluation details.
   Introduce a schema change for `core.rule_executions`, then update repository insert and select logic to store and read the evaluation payload.

5. Expose evidence through the decision detail endpoint.
   Update the HTTP DTOs and adapters so `GET /v1/tenants/:tenantId/decisions/:decisionId` returns rule evaluation details for each rule execution.

6. Decide whether to return raw evaluation only or merged definition-plus-evaluation.
   Raw evaluation gives parity at the storage/API layer. Merged definition plus evaluation is more directly useful for the frontend and makes threshold explanations easier to render.

7. Update the frontend decision detail page.
   Render evidence for triggered rules, starting with comparison cases where actual values and threshold constants can be shown directly.

8. Add tests and documentation.
   Cover capture, persistence, API serialization, and frontend rendering, then document the behavior and response contract.

## Checklist

- [x] Define the exact API contract for rule execution evidence
- [x] Decide between raw evaluation DTO and merged definition/evaluation DTO
- [x] Identify all `RuleExecution` structs in `new` that need expansion
- [x] Add evaluation fields to the domain model
- [x] Add a database migration for persisted rule evaluation data
- [x] Update `internal/store/postgres/rule_execution_repository.go` insert/select logic
- [x] Populate evaluation details during decision execution
- [x] Return evaluation details from `DecisionService.GetDecision`
- [x] Update `internal/httpapi/dto/decision.go` adapters and response types
- [x] Add backend unit tests for evaluation capture
- [x] Add repository tests for evaluation persistence
- [x] Add handler/API tests for the decision detail response shape
- [x] Update frontend API types in `new/frontend/src/lib/decision-engine-api.ts`
- [x] Render rule-hit evidence in the decision detail page
- [x] Add frontend test coverage or manual verification notes
- [x] Verify non-hit, snoozed, and error rule states still render correctly
- [x] Check payload size impact for decisions with many rules
- [x] Decide whether decision list endpoints should remain summary-only
- [x] Document the final response shape and behavior

## Recommended Sequence

1. Backend contract and model changes
2. Persistence and migration
3. API exposure
4. Frontend rendering
5. Tests and documentation

## Initial File Targets

- `new/backend/decision-engine-service/internal/service/rule_evaluation.go`
- `new/backend/decision-engine-service/internal/service/decision_service.go`
- `new/backend/decision-engine-service/internal/domain/decision/...`
- `new/backend/decision-engine-service/internal/store/postgres/rule_execution_repository.go`
- `new/backend/decision-engine-service/internal/httpapi/dto/decision.go`
- `new/frontend/src/lib/decision-engine-api.ts`
- `new/frontend/src/components/detection/decision-detail-page.tsx`

## Notes

- The current new service only returns summary rule execution fields.
- The legacy Marble API stores and exposes per-rule evaluation data.
- The main design decision is whether the new API should expose raw evaluation trees, merged definition/evaluation trees, or both.
- Implemented approach: persist a merged evaluation snapshot on each rule execution so decision reads do not need to reconstruct evidence from current rule definitions.
- Payload-size control: store and return evaluation snapshots only for `hit` rule executions. `no_hit` and `snoozed` executions remain summary-only.
- Verification run:
  - `go test ./internal/service ./internal/runtime/ast_eval ./internal/httpapi/handlers ./internal/store/postgres`
  - `cmd /c npm.cmd exec tsc --noEmit`
