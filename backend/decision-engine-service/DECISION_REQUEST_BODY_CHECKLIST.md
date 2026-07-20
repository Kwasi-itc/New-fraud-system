# Decision Request Body Checklist

## Goal

Show the full evaluated request body on decision details in the frontend by persisting it on the decision record and exposing it through the decision-detail endpoint.

## Backend persistence

- [x] Add `request_body` to `core.decisions`
- [x] Update the decision domain model to carry persisted request payloads
- [x] Persist the evaluated request body when a decision is created
- [x] Use the fully evaluated request body, including fields loaded from storage when the caller omits them

## Backend API

- [x] Expose `request_body` on `GET /v1/tenants/{tenantId}/decisions/{decisionId}`
- [x] Keep list decision responses lean unless the extra payload is explicitly needed there
- [x] Update OpenAPI so the detail endpoint documents the request body

## Frontend

- [x] Add the request-body field to the decision detail client types
- [x] Render the evaluated request body in the decision details panel
- [x] Show both a readable summary and the raw JSON

## Verification

- [x] Add or update backend tests for request-body persistence or response adaptation
- [x] Run `go test ./internal/... ./cmd/...` in `decision-engine-service`
- [x] Run the relevant frontend test or lint/build check if available

## Done when

- [x] Decision detail responses include the evaluated request body
- [x] The decisions UI shows the full request body clearly
- [x] Tests pass

## Verification note

- Backend Go tests passed on July 20, 2026.
- Frontend lint passed on July 20, 2026 with `npm.cmd run lint -- src/components/detection/scenario-edit-page.tsx src/lib/decision-engine-api.ts`.
