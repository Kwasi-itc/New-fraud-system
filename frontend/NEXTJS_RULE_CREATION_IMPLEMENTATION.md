# Next.js Rule Creation Implementation

This document translates the legacy `front/` rule authoring ideas into a practical implementation plan for the current `new/frontend` Next.js app.

It is intentionally grounded in:

- the current `new/frontend` App Router structure
- the current standalone `decision-engine-service` API
- the current standalone `data-model-service` API
- the React Query client architecture already present in the app

This is the Next.js version of the earlier legacy rule creation reference.

## Goal

Build a rule authoring experience in `new/frontend` that:

- matches the legacy Marble UX pattern more closely
- uses Next.js App Router correctly
- uses the standalone backend APIs we already have
- avoids monolith-specific assumptions from `front/`
- preserves room for a simple mode and an advanced mode

## Current State In `new/frontend`

There are already two rule-related flows in the new app.

### 1. Scenario edit page modal flow

File:

- `new/frontend/src/components/detection/scenario-edit-page.tsx`

Current behavior:

- lists rules inside the scenario editor
- can open `CreateRuleModal`
- supports a simple field/operator/value authoring pattern
- uses:
  - `decisionEngineApi.listRuleFunctions()`
  - `decisionEngineApi.listEditorIdentifiers()`
  - `decisionEngineApi.validateIteration()`
  - `decisionEngineApi.updateRule()`

This is currently the more integrated backend-aware flow.

### 2. Dedicated rule detail page flow

Route:

- `new/frontend/src/app/(authenticated)/detection/[scenarioId]/edit/rules/[ruleId]/page.tsx`

Component:

- `new/frontend/src/components/detection/rule-detail-page.tsx`

Current behavior:

- already uses a dedicated page
- visually tries to resemble the old UI
- is not yet aligned with the standalone backend AST contract
- currently builds a custom `formula` object with `operator/groups/conditions`
- does not yet use the backend AST function model properly

This means the right strategy is not to invent a third flow.

The right strategy is:

- keep the dedicated page idea
- use the modal only as a simple helper or retire it
- rebuild the dedicated page around the real backend contract

## Next.js Architectural Baseline

The app is built with App Router and client components for the interactive screens.

Relevant route entrypoints:

- `src/app/(authenticated)/detection/page.tsx`
- `src/app/(authenticated)/detection/[scenarioId]/edit/page.tsx`
- `src/app/(authenticated)/detection/[scenarioId]/edit/rules/[ruleId]/page.tsx`

These route files are very thin. They pass route params into client components.

That is a good pattern for this use case because:

- the rule editor is highly interactive
- it depends on React Query
- it needs popovers, local state, optimistic or deferred UX, and debounced validation

React Query is already globally available through:

- `src/components/providers.tsx`

So the new rule flow should continue using:

- App Router page file as thin wrapper
- client component for the real screen
- React Query for remote state
- local component state for editing buffers

## Desired Next.js Rule Authoring Flow

The flow should mirror the legacy product pattern while staying native to this codebase.

### Step 1. Rules list remains inside scenario editor

Keep the list of rules inside:

- `scenario-edit-page.tsx`

This page should handle:

- list rules
- search rules
- filter by rule group
- show rule score
- show validation status
- trigger create rule

It should not be the full authoring workspace.

### Step 2. Create a draft-safe rule on the backend

From the rules list page:

- create a minimal valid rule
- redirect to the dedicated rule detail page

This mirrors the old `front/` flow and works well in Next.js.

### Step 3. Edit on the dedicated rule detail page

Use:

- `/detection/[scenarioId]/edit/rules/[ruleId]`

as the real authoring page.

That page should own:

- rule metadata
- formula builder
- validation state
- save/update/delete behavior

## Backend Endpoints We Already Have

These are the core endpoints the Next.js flow should use.

### Scenario and iteration context

- `GET /v1/tenants/{tenantId}/scenarios/{scenarioId}`
- `GET /v1/tenants/{tenantId}/scenarios/{scenarioId}/iterations`
- `GET /v1/tenants/{tenantId}/scenarios/{scenarioId}/iterations/{iterationId}`

Used for:

- loading the scenario name
- determining draft iteration
- identifying whether authoring is allowed
- loading thresholds and trigger context if needed

### Rule CRUD

- `GET /v1/tenants/{tenantId}/scenarios/{scenarioId}/iterations/{iterationId}/rules`
- `POST /v1/tenants/{tenantId}/scenarios/{scenarioId}/iterations/{iterationId}/rules`
- `PUT /v1/tenants/{tenantId}/scenarios/{scenarioId}/iterations/{iterationId}/rules/{ruleId}`
- `DELETE /v1/tenants/{tenantId}/scenarios/{scenarioId}/iterations/{iterationId}/rules/{ruleId}`

Used for:

- list page
- blank rule creation
- detail page updates
- delete actions

### Validation and builder metadata

- `GET /v1/rule-functions`
- `POST /v1/tenants/{tenantId}/scenarios/{scenarioId}/iterations/{iterationId}/validate`
- `GET /v1/tenants/{tenantId}/scenarios/{scenarioId}/editor-identifiers`

Used for:

- function/operator catalog
- validation panel
- field/accessor pickers

### Data model context

- `GET /v1/tenants/{tenantId}/data-model`

Used for:

- object types
- fields
- links
- related-field paths
- trigger-object understanding

## Recommended Next.js Screen Structure

## 1. Rules list screen

Keep it inside:

- `src/components/detection/scenario-edit-page.tsx`

Recommended responsibilities:

- query scenario
- query iterations
- query current rules
- query validation for the current iteration
- derive rule groups from loaded rules
- show Add button
- navigate to rule detail after create

Recommended behavior for Add:

- remove the current full rule modal as the primary path
- replace with:
  - quick create button
  - optional small menu with:
    - `Quick rule`
    - `Advanced rule`

In practice both options can still create a blank rule and route to the detail page. The difference can be expressed by UI mode on that page later.

## 2. Rule detail screen

Use:

- `src/components/detection/rule-detail-page.tsx`

but refactor it into a true backend-driven editor.

Recommended sections:

### Header

- back button to scenario rules list
- rule name
- rule state badge
- actions:
  - save
  - duplicate later if implemented
  - delete

### Metadata section

- description
- rule group
- score modifier
- stable rule id
- snooze group id

### Formula section

- mode switch:
  - simple
  - advanced

### Validation section

- top-level iteration/rule validation messages
- function-level feedback where possible

### Optional later

- AI helper area

## State Management Model

Because this is Next.js plus React Query, split state into two categories.

### Remote state

Use React Query for:

- scenario
- iterations
- rules
- rule functions
- editor identifiers
- assembled data model
- iteration validation

These should remain canonical server state.

### Local draft state

Use component state for:

- editable metadata fields
- in-progress formula tree
- pending validation rendering
- open selectors/popovers
- mode toggle between simple and advanced editing

Do not mutate React Query cache directly as the primary draft buffer for the formula editor.

The detail page should load server state once, then stage edits locally until save.

## Recommended Query Composition

Create one rule-editor composition hook instead of fetching everything in unrelated places.

Suggested hook:

- `useRuleEditorContext(tenantId, scenarioId, ruleId?)`

It should internally combine:

- `decisionEngineApi.getScenario`
- `decisionEngineApi.listIterations`
- `decisionEngineApi.listRuleFunctions`
- `decisionEngineApi.listEditorIdentifiers`
- `useAssembledDataModelQuery`
- current rule lookup from the active draft iteration

Return shape should be something like:

- `scenario`
- `draftIteration`
- `rule`
- `ruleFunctions`
- `payloadAccessors`
- `databaseAccessors`
- `dataModel`
- `triggerObjectType`
- `ruleGroups`
- `validation`

This is the Next.js replacement for the old monolith builder-options resource route.

## Builder Options In Next.js

The old app had a dedicated `builder-options` resource route.

In `new/frontend`, do not recreate that as a local Next API route unless there is a strong need.

Instead, compose the options client-side from the APIs we already have.

Recommended builder options object:

```ts
type RuleBuilderOptions = {
  triggerObjectType: string;
  ruleFunctions: RuleFunction[];
  payloadAccessors: ASTNodeDTO[];
  databaseAccessors: ASTNodeDTO[];
  assembledModel: AssembledDataModel;
  ruleGroups: string[];
};
```

This gives the rule editor the same ergonomic input the old AST builder expected, but using current standalone endpoints.

## Formula Model: What Must Change

This is the most important implementation point.

The standalone backend expects AST-shaped JSON nodes:

- `function`
- `constant`
- `children`
- `named_children`

The current `rule-detail-page.tsx` does not build that shape. It builds a UI-only object with:

- `operator`
- `groups`
- `conditions`

That shape must be replaced.

### Correct direction

The editor UI can still present:

- left operand
- operator
- right operand
- groups
- `and`
- `or`

But it must compile those controls into backend AST before save.

### Example mapping

UI condition:

- left: `amount`
- operator: `>`
- right: `1000`

Should compile into:

```json
{
  "function": "gt",
  "children": [
    {
      "function": "field_ref",
      "named_children": {
        "field": { "constant": "amount" }
      }
    },
    { "constant": 1000 }
  ]
}
```

Multiple conditions in an AND group:

```json
{
  "function": "and",
  "children": [ ...conditions ]
}
```

Multiple groups joined by OR:

```json
{
  "function": "or",
  "children": [ ...groupNodes ]
}
```

This compiler layer is the key bridge between the old-style visual UI and the standalone backend.

## Recommended Editing Modes

To keep the UI practical, support two modes on the dedicated rule page.

### Simple mode

Simple mode should feel closer to the current modal:

- one or more conditions
- grouped by `and` / `or`
- dropdown field selectors
- operator selectors
- constant or accessor value selectors

This will handle the majority of authoring cases.

### Advanced mode

Advanced mode should be backend-driven.

It should use:

- `listRuleFunctions()`
- accessors
- data-model paths

This does not need to be a full clone of the old AST builder on day one.

The minimal advanced mode could be:

- function picker
- named arg builder
- child node nesting
- JSON preview

That is enough to unlock the backend’s power incrementally.

## Validation Flow In Next.js

The standalone backend validates at the iteration level, not just a single detached AST node.

Use:

- `decisionEngineApi.validateIteration(tenantId, scenarioId, iterationId)`

Recommended pattern:

1. Save rule changes.
2. Re-fetch rule list or current rule.
3. Trigger iteration validation.
4. Surface:
   - global iteration validity
   - current rule’s validation result

### Suggested UX

On the rule detail page:

- show a compact validation panel under the formula editor
- when validation is running, show a pending state
- when validation fails, highlight only the current rule’s errors prominently
- optionally show broader iteration trigger errors separately

### Suggested trigger timing

Do not call validation on every keystroke at first.

Use one of:

- explicit Validate button
- debounced validation after formula changes
- validation after save

The safest first implementation is validation after save plus a manual Validate button.

## Rule Creation Strategy

The backend requires:

- `display_order`
- `name`
- `description`
- `formula`
- `score_modifier`
- `rule_group`
- `stable_rule_id`

Optional:

- `snooze_group_id`

Because creation only works on draft iterations, the list page should first determine the draft target iteration.

### Recommended blank rule payload

Use a draft-safe starter payload like:

```json
{
  "display_order": nextDisplayOrder,
  "name": "New rule",
  "description": "",
  "formula": { "constant": true },
  "score_modifier": 0,
  "rule_group": "",
  "stable_rule_id": "new-rule"
}
```

Why `{"constant": true}`:

- it is AST-compatible
- it is easy to replace immediately
- it avoids sending an invalid ad hoc formula object

If the backend or product wants “empty formula” semantics instead, that should be decided explicitly. Until then, use valid AST.

## Rule Group UX In Next.js

The old UI had a better group selection pattern than the current new app.

Recommended replacement for plain text:

- popover selector
- searchable list of existing groups
- create new group inline
- clear selection

This can be implemented as a small standalone component in `new/frontend` without importing the old monolith implementation.

Suggested component:

- `src/components/detection/rule-group-picker.tsx`

Inputs:

- `value`
- `groups`
- `onChange`
- `disabled`

## Suggested File Structure

Recommended additions/refactors:

- `src/components/detection/rule-detail-page.tsx`
  - refactor into real backend-driven editor
- `src/components/detection/rule-group-picker.tsx`
  - legacy-inspired rule group selector
- `src/components/detection/rule-validation-panel.tsx`
  - renders current rule validation
- `src/components/detection/rule-builder-simple.tsx`
  - simple grouped conditions editor
- `src/components/detection/rule-builder-advanced.tsx`
  - function/AST-driven editor
- `src/components/detection/rule-builder-shared.ts`
  - AST compile/parse helpers
- `src/lib/decision-engine-query.ts`
  - optional React Query hook layer if desired
- `src/lib/rule-builder.ts`
  - AST helper functions

## Suggested Helper Functions

Create reusable helpers for:

- `buildFieldRef(fieldName)`
- `buildConstant(value)`
- `buildComparison(operator, leftNode, rightNode)`
- `buildAnd(children)`
- `buildOr(children)`
- `compileConditionGroupsToAst(conditionGroups)`
- `tryParseSimpleAstToConditionGroups(ast)`
- `slugifyStableRuleId(name)`

That parser/compiler pair is what will let the UI:

- show simple mode for supported rules
- detect unsupported AST shapes
- fall back to advanced mode or read-only display when needed

## Practical Migration Plan

## Phase 1

- keep `scenario-edit-page.tsx` as the rules list page
- remove modal-as-primary flow
- create blank rule via backend and navigate to detail page
- fix `rule-detail-page.tsx` to save real AST

## Phase 2

- add rule-group picker
- add validation panel
- add save + validate workflow
- derive rule groups from current iteration rules

## Phase 3

- add simple mode compiler/parser
- add multiple AND/OR groups
- support field accessors and value type coercion properly

## Phase 4

- add advanced mode using `listRuleFunctions`
- add richer function-based constructs:
  - `related_count`
  - `related_field`
  - `in_custom_list`
  - `past_decision_count`

## Phase 5

- optional AI helper layer if backend endpoints are introduced for it

## What We Should Not Do

Do not:

- copy the old Remix route architecture into Next.js
- keep two competing rule authoring experiences long-term
- send UI-only formula objects to the standalone backend
- hardcode all rule functions when the backend already exposes a catalog
- bury advanced authoring inside a modal

## Final Recommendation

For Next.js, the correct adaptation of the old Marble rule UX is:

- use App Router pages as route wrappers
- keep React Query in client components
- use a dedicated rule detail page as the main authoring workspace
- compile visual condition editing into standalone backend AST
- build a small Next.js-native builder-options composition layer from existing endpoints
- add validation as a first-class part of the rule detail page

That gives us the old product’s strengths while staying aligned with:

- the current `new/frontend` structure
- the standalone decision-engine service
- the standalone data-model service

It also means we can evolve from a simple builder into a full AST-capable editor without discarding the work.
