# Legacy Rule Creation UI Reference

This document describes how the legacy rule creation and rule editing experience is implemented in the old `front/` app so the new `new/frontend/` app can selectively reuse the same ideas.

The goal here is not to mirror every file mechanically. The goal is to capture:

- the user flow
- the route flow
- the backend/resource dependencies
- the UI composition
- the AST builder pattern
- the validation and AI-assist pattern
- the practical carryover items for the new frontend

## Scope

This document focuses on the legacy detection scenario rule authoring flow under:

- `front/packages/app-builder/src/routes/_builder+/detection+/scenarios+/$scenarioId+/i+/$iterationId+/...`
- `front/packages/app-builder/src/components/Scenario/Rules/...`
- `front/packages/app-builder/src/components/AstBuilder/...`
- `front/packages/app-builder/src/routes/ressources+/scenarios+/...`

Primary legacy files reviewed:

- `front/packages/app-builder/src/routes/_builder+/detection+/scenarios+/$scenarioId+/i+/$iterationId+/_edit-view+/rules.tsx`
- `front/packages/app-builder/src/components/Scenario/Rules/Actions/CreateRule.tsx`
- `front/packages/app-builder/src/routes/ressources+/scenarios+/$scenarioId+/$iterationId+/rules+/create.tsx`
- `front/packages/app-builder/src/routes/_builder+/detection+/scenarios+/$scenarioId+/i+/$iterationId+/rules.$ruleId.tsx`
- `front/packages/app-builder/src/components/Scenario/Screening/FieldAstFormula.tsx`
- `front/packages/app-builder/src/components/AstBuilder/Provider.tsx`
- `front/packages/app-builder/src/routes/ressources+/scenarios+/$scenarioId+/builder-options.tsx`
- `front/packages/app-builder/src/routes/ressources+/scenarios+/$scenarioId+/validate-ast.tsx`
- `front/packages/app-builder/src/components/Scenario/Screening/FieldRuleGroup.tsx`
- `front/packages/app-builder/src/components/Scenario/Rules/AiGenerateRule.tsx`
- `front/packages/app-builder/src/queries/scenarios/generate-rule.ts`
- `front/packages/app-builder/src/queries/scenarios/rule-description.ts`

## High-Level Design

The legacy app does not treat "create rule" as a modal form with all fields inline.

Instead, it uses a two-step flow:

1. Create a blank rule record.
2. Redirect the user to a dedicated rule detail page where the actual authoring happens.

This is the biggest architectural difference from the current `new/frontend` implementation.

In the legacy app:

- list page is lightweight
- create action is lightweight
- rule detail page is the real authoring workspace
- AST editing is first-class
- validation is live
- AI helpers are attached to the detail page, not the list page

## User Flow

### 1. Rules list page

The scenario iteration rules list page lives at:

- `front/packages/app-builder/src/routes/_builder+/detection+/scenarios+/$scenarioId+/i+/$iterationId+/_edit-view+/rules.tsx`

Behavior:

- shows all rules and screening configs in one table
- supports search
- supports rule-group filtering
- shows validation error indicators per item
- exposes an "Add" dropdown
- add dropdown contains:
  - create rule
  - create screening

Important characteristics:

- rule creation starts from the list page
- actual editing happens after navigation
- the page computes available `ruleGroups` from existing items so the detail page can reuse them

### 2. Create rule action

The button component is:

- `front/packages/app-builder/src/components/Scenario/Rules/Actions/CreateRule.tsx`

This button does not collect form data.

It simply calls a mutation:

- `front/packages/app-builder/src/queries/scenarios/create-rule.ts`

That mutation POSTs to:

- `/ressources/scenarios/:scenarioId/:iterationId/rules/create`

### 3. Blank rule seed route

The server route that actually creates the initial rule is:

- `front/packages/app-builder/src/routes/ressources+/scenarios+/$scenarioId+/$iterationId+/rules+/create.tsx`

It seeds a rule with:

- `displayOrder: 1`
- `formula: null`
- default translated name
- empty description
- empty rule group
- `scoreModifier: 0`

Then it redirects to the rule detail page.

This matters because the old UX assumes:

- a rule can exist before the formula is authored
- incomplete rules are acceptable in draft state
- the detail page is responsible for turning a placeholder rule into a valid one

### 4. Dedicated rule detail page

The actual rule editor is:

- `front/packages/app-builder/src/routes/_builder+/detection+/scenarios+/$scenarioId+/i+/$iterationId+/rules.$ruleId.tsx`

This page is the core of the old rule-authoring system.

It provides:

- editable name
- editable description
- editable rule group
- full AST formula editor
- score modifier editor
- live validation feedback
- AI-generated rule description
- AI-generated formula suggestions
- duplicate rule action
- delete rule action

## Rule Detail Page Structure

The rule detail page is not a generic form. It is a dedicated authoring workspace with three layers.

### Layer 1: metadata fields

These are plain fields:

- `name`
- `description`
- `ruleGroup`
- `scoreModifier`

Validation is local and form-driven using `zod` plus TanStack Form.

Local schema:

- `name`: required
- `description`: optional
- `ruleGroup`: optional
- `scoreModifier`: integer between `-1000` and `1000`
- `formula`: unrestricted locally, with semantic validation delegated to backend validation

### Layer 2: AST formula editing

This is the real heart of the experience.

The rule formula field is rendered through:

- `FieldAstFormula`

File:

- `front/packages/app-builder/src/components/Scenario/Screening/FieldAstFormula.tsx`

That component wraps:

- `AstBuilder.Provider`
- `AstBuilder.Root`

The editor uses:

- scenario-aware builder options
- trigger object type
- data model
- payload accessors
- database accessors
- custom lists
- screening configs
- feature access flags

This means the formula builder is not static. It is driven by live backend context.

### Layer 3: assistive intelligence

The detail page also layers in AI helpers:

- `AiDescription`
- `AiGenerateRule`

Files:

- `front/packages/app-builder/src/components/Scenario/Rules/AiGenerateRule.tsx`
- `front/packages/app-builder/src/queries/scenarios/generate-rule.ts`
- `front/packages/app-builder/src/queries/scenarios/rule-description.ts`

The legacy UI uses AI in two ways:

1. Explain an authored formula in natural language.
2. Generate a formula from a user instruction.

This is important because the old system treated AI as augmentation of the AST builder, not a replacement for it.

## Data Loading Pattern

The rule detail loader fetches all authoring context up front.

Key data loaded in the page loader:

- `databaseAccessors`
- `payloadAccessors`
- `dataModel`
- `customLists`
- `rule`
- `screeningConfigs`
- entitlement-derived feature flags

This comes from:

- editor accessor service
- data model repository
- custom lists repository
- rule repository
- continuous screening repository when available

The page then constructs one `options` object and passes it into `FieldAstFormula`.

That pattern is worth preserving:

- load all authoring dependencies once
- centralize them in one rule-builder options object
- hand them to the rule editor instead of scattering fetches across nested components

## Builder Options Contract

The legacy builder options resource route is:

- `front/packages/app-builder/src/routes/ressources+/scenarios+/$scenarioId+/builder-options.tsx`

Returned shape:

- `customLists`
- `triggerObjectType`
- `dataModel`
- `databaseAccessors`
- `payloadAccessors`
- `hasValidLicense`
- `hasContinuousScreening`
- `screeningConfigs`

This route is critical because it is the contract that powers the AST builder UI.

Conceptually, this route answers:

- what object type are we building against?
- what fields exist on the trigger object?
- what related objects and access paths are available?
- what lists and configs can be referenced?
- what features should the editor expose?

For the new frontend, this maps closely to the newer decision-engine and data-model endpoints.

## AST Builder Pattern

The legacy app has a dedicated AST builder subsystem:

- `front/packages/app-builder/src/components/AstBuilder/...`

The reviewed integration points show a few important design choices.

### Provider pattern

`AstBuilder.Provider`:

- loads or receives builder options
- computes `triggerObjectTable`
- stores state in a dedicated sharpstate store
- syncs builder option changes into the store

This means the builder is context-driven, not prop-drilled one node at a time.

### Root pattern

`AstBuilder.Root` receives:

- the current AST node
- `onUpdate`
- `onValidationUpdate`
- `returnType="bool"`

That is a strong pattern to keep:

- authoring should know the expected output type
- validation feedback should be pushed back into the surrounding page
- the page should own persistence, not the AST builder itself

### Undefined vs empty formula

Legacy code explicitly distinguishes:

- undefined/null trigger or rule AST
- empty starter AST node

Examples:

- `NewEmptyRuleAstNode()`
- `NewEmptyTriggerAstNode()`
- `NewUndefinedAstNode()`

That distinction matters for UX:

- empty formula means "start editing from a default skeleton"
- undefined formula means "no formula authored yet"

The old app uses this to support draft-safe authoring.

## Validation Pattern

Legacy validation is live and server-backed.

The validation route is:

- `front/packages/app-builder/src/routes/ressources+/scenarios+/$scenarioId+/validate-ast.tsx`

The corresponding client query is:

- `front/packages/app-builder/src/queries/validate-ast.ts`

The payload contains:

- `node`
- `expectedReturnType`

Returned data is transformed into a flattened structure:

- top-level validation errors
- per-node evaluation rows
- related node ids

This flattening is important because it lets the UI:

- render formula-level errors
- render node-level errors
- suppress noisy type errors when more meaningful nested errors exist

The logic in `FieldAstFormula.tsx` intentionally filters some high-level boolean-return errors when deeper nested node errors are already present.

That is a high-quality UX decision worth reusing.

## Rule Group UX

Rule groups are not a plain text input in the old app.

They use:

- `front/packages/app-builder/src/components/Scenario/Screening/FieldRuleGroup.tsx`

Behavior:

- popover-based selector
- can pick an existing group
- can clear a selected group
- can create a new group inline
- merges:
  - selected group
  - newly typed group
  - existing groups

This is materially better than a free text field because it:

- preserves consistency
- prevents spelling drift
- still allows new groups when needed

## AI Assistance Pattern

The old app has two distinct AI helpers.

### 1. AI rule description

The detail page calls a mutation after formula changes, debounced by 3 seconds.

Purpose:

- transform current AST into a readable natural-language description

Behavior:

- only runs when AI entitlement is enabled
- only updates the description if the rule is valid
- displays pending state while debouncing or waiting for the server

This is a very good assistive pattern because it helps authors verify intent without changing the underlying rule.

### 2. AI formula generation

`AiGenerateRule.tsx`:

- accepts free text instruction
- sends `rule_id` and `instruction`
- receives generated AST
- injects AST into the form
- re-mounts the formula builder so the new AST renders cleanly

This is an important implementation detail:

- generated formula is not stored in isolated AI state
- it is written directly into the same `formula` field as manual authoring
- manual and AI editing converge on the same AST representation

That is the correct architecture for AI-assisted rule building.

## Persistence Pattern

The rule detail page persists through one form submit action, not autosave.

Update action:

- validates locally with Zod
- sends the normalized rule payload to the server
- server updates the rule
- toast feedback is returned

This gives the old UI:

- live semantic validation for guidance
- explicit save for persistence

That separation is useful:

- validation does not imply save
- save does not require complex local draft buffering

## Legacy Rule Model Assumptions

From the old UI, the practical rule model is:

- `id`
- `name`
- `description`
- `ruleGroup`
- `scoreModifier`
- `formula`
- `displayOrder`

The old UI reviewed here does not expose `stable_rule_id` or `snooze_group_id` in the main authoring experience.

That is an important difference relative to the new standalone decision-engine backend.

For the new system, the backend supports more rule metadata than the old UI surfaced.

## What The Old UI Did Well

The strongest patterns worth carrying over:

- dedicated rule detail page instead of squeezing everything into a modal
- create-blank-then-edit workflow
- full AST editor instead of only a simple operator form
- server-driven builder options
- server-backed live validation
- clean separation between metadata editing and formula editing
- reusable rule-group selector with inline create
- AI description as verification aid
- AI formula generation into the same AST field
- explicit save instead of autosaving every edit

## Where The Old UI Was Constrained

Things to be careful about if copying it directly:

- it depends on a large custom AST builder subsystem
- it assumes old app-builder models and repositories
- it is coupled to Remix resource routes
- feature access and entitlements are threaded through many places
- the create route seeds placeholder data that may not align exactly with the new backend contract
- it was designed around the monolith-era API surface, not the standalone service boundaries

## Mapping To The New Standalone Backend

The new standalone decision-engine service already has the right conceptual backend pieces:

- rule CRUD
- iteration validation
- rule function catalog
- AST JSON formula model

But the current `new/frontend` UI only exposes a simple rule modal and a subset of formula authoring.

The biggest gap is not backend capability. The biggest gap is frontend authoring depth.

Legacy-to-new conceptual mapping:

- legacy builder options route
  - should map to a composed frontend data source built from:
    - decision-engine `GET /v1/rule-functions`
    - decision-engine scenario/iteration/rule endpoints
    - data-model assembled model endpoint
    - optional platform/helper endpoints as needed

- legacy validate AST route
  - should map to:
    - decision-engine `POST /v1/tenants/{tenantId}/scenarios/{scenarioId}/iterations/{iterationId}/validate`

- legacy create blank rule route
  - should map to:
    - decision-engine rule create with a valid draft-safe starter payload

## Recommended Carryover Plan For `new/frontend`

### Recommended immediately

- move rule creation out of the small inline modal for advanced authoring
- introduce a dedicated rule detail page
- keep the current modal only for a quick-start mode, if desired
- add a server-driven rule function catalog query from `GET /v1/rule-functions`
- add a richer builder-options composition layer in `new/frontend`
- add explicit validate action wired to iteration validation

### Recommended UI structure

Suggested new structure:

1. rules list page
   - search
   - group filter
   - create rule button

2. create rule action
   - create minimal draft-safe rule on backend
   - redirect to detail page

3. rule detail page
   - name
   - description
   - rule group
   - score modifier
   - stable rule id
   - snooze group id
   - formula editor
   - validation panel
   - optional AI tools

### Recommended authoring modes

Support two rule-authoring modes:

- simple mode
  - field / operator / value
  - similar to current `CreateRuleModal`

- advanced mode
  - full AST builder
  - function catalog driven

This gives a better progression than forcing all users into either the old full builder or the current oversimplified modal.

## Concrete Takeaways

If the new frontend wants to borrow the legacy design intelligently, the main things to replicate are:

- the page flow, not the exact UI library
- the builder-options contract idea
- the validation feedback loop
- the dedicated formula workspace
- AI as augmentation of the AST field

The main things not to copy blindly are:

- Remix-specific resource route structure
- monolith-specific repositories
- exact component tree depth
- older entitlement plumbing

## Summary

The old `front/` rule creation UX was not a "form popup." It was a full authoring system built around:

- a dedicated rule page
- an AST builder
- live server validation
- context-driven builder options
- reusable grouping and filtering patterns
- optional AI support for explanation and generation

That is the right mental model to bring into `new/frontend`.

If we want parity with the power of the old app while staying aligned with the standalone backend, the next frontend should be designed as:

- a dedicated rule authoring workspace
- backed by decision-engine AST/function contracts
- enriched by data-model context
- with a simple mode layered on top, not instead of it
