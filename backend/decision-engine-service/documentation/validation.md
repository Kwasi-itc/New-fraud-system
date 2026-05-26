# Validation

This document explains the iteration validation endpoints.

## Endpoint Group

- `GET /v1/rule-functions`
- `POST /v1/tenants/:tenantId/scenarios/:scenarioId/iterations/:iterationId/validate`

Primary files:

- [internal/httpapi/handlers/validation.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/decision-engine-service/internal/httpapi/handlers/validation.go)
- [internal/service/validation_service.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/decision-engine-service/internal/service/validation_service.go)

## Parameters

- `tenantId`
  - tenant whose model contract will be used for validation
- `scenarioId`
  - scenario that owns the iteration being validated
- `iterationId`
  - exact iteration to validate

## What Validation Means

The service validates:

- trigger object compatibility
- rule references against the tenant data model
- AST field and relation references
- evaluator assumptions that depend on the assembled model from `data-model-service`

The response may still be `200` or `422` depending on whether validation completed cleanly or returned structured errors.

## Endpoint Detail

### `GET /v1/rule-functions`

What it does:

- returns the catalog of supported AST rule functions

What the response includes:

- function name
- category
- description
- return type
- whether the function uses positional children or named children
- expected arguments
- whether the function depends on:
  - tenant model semantics
  - tenant data reads
  - platform helper repositories
- an example AST snippet

How it works:

- the handler returns a catalog built from the evaluator's supported function set
- this keeps the frontend's rule builder aligned with the real runtime instead of relying on a separate handwritten list

How it should be used:

- frontend rule builders
- operator/function pickers
- dynamic forms for named arguments like `field`, `path`, `object_type`, `equals`, and `flag`
- inline help and examples while authors build rules

When to call it:

- when the rule builder loads
- when the frontend refreshes or caches supported authoring options

### `POST /v1/tenants/:tenantId/scenarios/:scenarioId/iterations/:iterationId/validate`

What it does:

- validates one scenario iteration exactly as authored
- checks the iteration trigger plus all rules under it

How it works:

1. loads the tenant scenario
2. loads the target iteration
3. loads the rules attached to that iteration
4. fetches the published tenant model from `data-model-service`
5. validates AST references and scenario semantics against that model
6. returns a structured validation report

How it should be used:

- before committing an iteration
- before starting publication preparation
- in authoring UIs after rule edits
- in CI or release workflows that want to block invalid scenario versions

When to call it:

- after any trigger change
- after any rule formula change
- after tenant model changes that could invalidate scenario references
