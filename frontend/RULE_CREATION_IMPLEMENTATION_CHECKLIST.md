# Rule Creation Implementation Checklist

This checklist is the execution plan for implementing rule creation and rule editing in `new/frontend` using:

- Next.js App Router
- React Query
- the standalone `decision-engine-service`
- the standalone `data-model-service`

It is based on:

- the legacy `front/` rule authoring flow
- the current `new/frontend` structure
- the backend contracts already available in `new/frontend/src/lib/decision-engine-api.ts`

## Implementation Goal

Build a rule authoring flow that:

- uses the dedicated rule detail page as the primary editing surface
- creates a blank draft-safe rule first, then navigates into editing
- saves real backend-compatible AST
- validates against the standalone decision engine
- reuses the old product’s UX ideas without copying monolith-specific code

## Phase 0: Scope And Decisions

- [ ] Confirm that the dedicated rule detail page will be the main authoring surface.
- [ ] Confirm that `CreateRuleModal` will no longer be the primary create/edit flow.
- [ ] Confirm that rules can only be created, edited, and deleted on the current draft iteration.
- [ ] Confirm the starter blank-rule payload shape.
- [ ] Decide whether starter `formula` should be:
  - [ ] `{"constant": true}`
  - [ ] another valid AST starter node
- [ ] Decide default values for:
  - [ ] `name`
  - [ ] `description`
  - [ ] `score_modifier`
  - [ ] `rule_group`
  - [ ] `stable_rule_id`

## Phase 1: Data And Query Layer

- [ ] Create a dedicated rule editor query/composition layer.
- [ ] Add a hook or module to combine:
  - [ ] `decisionEngineApi.getScenario`
  - [ ] `decisionEngineApi.listIterations`
  - [ ] `decisionEngineApi.listRules`
  - [ ] `decisionEngineApi.listRuleFunctions`
  - [ ] `decisionEngineApi.listEditorIdentifiers`
  - [ ] `decisionEngineApi.validateIteration`
  - [ ] `useAssembledDataModelQuery`
- [ ] Derive the current draft iteration from loaded iterations.
- [ ] Derive the current rule from rule id and draft iteration.
- [ ] Derive current rule groups from the loaded rule list.
- [ ] Derive trigger object type from scenario data.
- [ ] Derive payload field options from `editor-identifiers`.
- [ ] Confirm that the rule detail page can load all required authoring context from one place.

### Suggested files

- [ ] `src/lib/decision-engine-query.ts`
- [ ] `src/lib/rule-builder.ts`
- [ ] `src/lib/rule-builder-ast.ts`

## Phase 2: Create Rule Flow

- [ ] Refactor rule creation on `scenario-edit-page.tsx`.
- [ ] Replace modal-first creation with:
  - [ ] find draft iteration
  - [ ] create blank rule on backend
  - [ ] navigate to `/detection/[scenarioId]/edit/rules/[ruleId]`
- [ ] Compute `display_order` for the new rule.
- [ ] Generate `stable_rule_id` for the new rule.
- [ ] Handle “no draft iteration exists” cleanly.
- [ ] Show a clear message/toast if the user must create a draft first.
- [ ] Invalidate relevant queries after create.

### Files to touch

- [ ] `src/components/detection/scenario-edit-page.tsx`
- [ ] `src/lib/decision-engine-query.ts` or equivalent helper module

## Phase 3: Rule Detail Page Refactor

- [ ] Make `rule-detail-page.tsx` the canonical rule editing page.
- [ ] Remove the current UI-only formula payload shape:
  - [ ] `operator`
  - [ ] `groups`
  - [ ] `conditions`
- [ ] Replace it with real backend AST handling.
- [ ] Load the correct draft iteration for the rule.
- [ ] Load the current backend rule record.
- [ ] Stage editable metadata in local component state.
- [ ] Save rule updates using `decisionEngineApi.updateRule`.
- [ ] Delete rules using `decisionEngineApi.deleteRule`.
- [ ] Block edit/delete operations when the iteration is not draft.

### Rule metadata section

- [ ] Rule name
- [ ] Rule description
- [ ] Rule group
- [ ] Score modifier
- [ ] Stable rule id
- [ ] Snooze group id

## Phase 4: AST Helper Layer

- [ ] Add helper to build a field reference AST node.
- [ ] Add helper to build constant AST nodes.
- [ ] Add helper to build comparison AST nodes.
- [ ] Add helper to build `and` group AST nodes.
- [ ] Add helper to build `or` group AST nodes.
- [ ] Add helper to build list membership AST nodes.
- [ ] Add helper to slugify `stable_rule_id`.
- [ ] Add helper to compile simple grouped conditions into AST.
- [ ] Add helper to parse supported AST back into grouped-condition UI state.
- [ ] Standardize frontend AST output on `function`, not legacy `name`.

### Helper functions to add

- [ ] `buildFieldRef(fieldName)`
- [ ] `buildConstant(value)`
- [ ] `buildComparison(operator, leftNode, rightNode)`
- [ ] `buildAnd(children)`
- [ ] `buildOr(children)`
- [ ] `buildIn(leftNode, values)`
- [ ] `compileConditionGroupsToAst(conditionGroups)`
- [ ] `tryParseAstToConditionGroups(ast)`
- [ ] `slugifyStableRuleId(name)`

## Phase 5: Simple Rule Builder

- [ ] Build a `rule-builder-simple` component.
- [ ] Support grouped conditions with:
  - [ ] multiple conditions in a group
  - [ ] `and` inside a group
  - [ ] `or` between groups
- [ ] Support left operand selection.
- [ ] Support operator selection.
- [ ] Support right operand entry.
- [ ] Support right operand type selection:
  - [ ] string
  - [ ] number
  - [ ] boolean
  - [ ] list for `in`
- [ ] Compile UI state into backend AST before save.
- [ ] Parse supported backend AST into UI state when loading a rule.
- [ ] Show a fallback warning when an existing rule uses an unsupported AST shape.

### First-pass operators

- [ ] `eq`
- [ ] `neq`
- [ ] `gt`
- [ ] `gte`
- [ ] `lt`
- [ ] `lte`
- [ ] `contains`
- [ ] `starts_with`
- [ ] `ends_with`
- [ ] `in`

### Suggested files

- [ ] `src/components/detection/rule-builder-simple.tsx`
- [ ] `src/components/detection/rule-detail-page.tsx`

## Phase 6: Validation UX

- [ ] Add validation support to the rule detail page.
- [ ] Decide initial validation trigger strategy:
  - [ ] validate after save
  - [ ] manual Validate button
  - [ ] debounced validation later
- [ ] Render top-level iteration validation state.
- [ ] Render current rule validation state.
- [ ] Separate current rule errors from broader iteration trigger errors.
- [ ] Show loading/pending validation state.
- [ ] Revalidate after successful save.

### Suggested file

- [ ] `src/components/detection/rule-validation-panel.tsx`

## Phase 7: Rule Group Picker

- [ ] Replace plain text rule-group input with a picker.
- [ ] Support:
  - [ ] selecting an existing group
  - [ ] clearing the current group
  - [ ] creating a new group inline
  - [ ] searching existing groups
- [ ] Feed available groups from current iteration rules.

### Suggested file

- [ ] `src/components/detection/rule-group-picker.tsx`

## Phase 8: Rules List UX

- [ ] Keep the rules list in `scenario-edit-page.tsx`.
- [ ] Improve it to match the old `front` flow more closely.
- [ ] Ensure each rule row links to the dedicated rule detail page.
- [ ] Keep or improve:
  - [ ] search
  - [ ] rule group filter
  - [ ] score display
  - [ ] validation badges
- [ ] Add row actions only if they stay consistent with the dedicated page flow.

## Phase 9: Advanced Mode

- [ ] Add a mode switch to the rule detail page:
  - [ ] simple
  - [ ] advanced
- [ ] Build an advanced rule authoring mode driven by backend function catalog.
- [ ] Use `decisionEngineApi.listRuleFunctions()` as the source of truth.
- [ ] Use assembled data model and editor identifiers as context.
- [ ] Add support for richer backend functions over time:
  - [ ] `field_ref`
  - [ ] `related_count`
  - [ ] `related_field`
  - [ ] `in_custom_list`
  - [ ] `past_decision_count`
- [ ] Decide whether advanced mode starts as:
  - [ ] a structured function builder
  - [ ] an AST tree builder
  - [ ] a JSON editor with helper UI

### Suggested file

- [ ] `src/components/detection/rule-builder-advanced.tsx`

## Phase 10: Cleanup

- [ ] Decide final fate of `CreateRuleModal`:
  - [ ] remove it
  - [ ] keep it only as a quick-start helper
  - [ ] keep it only for supported simple edits
- [ ] Remove duplicate or conflicting rule flows.
- [ ] Audit all query invalidation after:
  - [ ] create
  - [ ] update
  - [ ] delete
  - [ ] validate
- [ ] Ensure only one canonical rule creation path remains.
- [ ] Ensure only one canonical rule editing path remains.

## File Checklist

### Existing files to refactor

- [ ] `src/components/detection/scenario-edit-page.tsx`
- [ ] `src/components/detection/rule-detail-page.tsx`
- [ ] `src/components/detection/create-rule-modal.tsx`

### New files to add

- [ ] `src/components/detection/rule-builder-simple.tsx`
- [ ] `src/components/detection/rule-builder-advanced.tsx`
- [ ] `src/components/detection/rule-group-picker.tsx`
- [ ] `src/components/detection/rule-validation-panel.tsx`
- [ ] `src/lib/decision-engine-query.ts`
- [ ] `src/lib/rule-builder.ts`
- [ ] `src/lib/rule-builder-ast.ts`

## Backend Endpoint Checklist

### Decision engine

- [ ] `GET /v1/tenants/{tenantId}/scenarios/{scenarioId}`
- [ ] `GET /v1/tenants/{tenantId}/scenarios/{scenarioId}/iterations`
- [ ] `GET /v1/tenants/{tenantId}/scenarios/{scenarioId}/iterations/{iterationId}`
- [ ] `GET /v1/tenants/{tenantId}/scenarios/{scenarioId}/iterations/{iterationId}/rules`
- [ ] `POST /v1/tenants/{tenantId}/scenarios/{scenarioId}/iterations/{iterationId}/rules`
- [ ] `PUT /v1/tenants/{tenantId}/scenarios/{scenarioId}/iterations/{iterationId}/rules/{ruleId}`
- [ ] `DELETE /v1/tenants/{tenantId}/scenarios/{scenarioId}/iterations/{iterationId}/rules/{ruleId}`
- [ ] `GET /v1/rule-functions`
- [ ] `GET /v1/tenants/{tenantId}/scenarios/{scenarioId}/editor-identifiers`
- [ ] `POST /v1/tenants/{tenantId}/scenarios/{scenarioId}/iterations/{iterationId}/validate`

### Data model

- [ ] `GET /v1/tenants/{tenantId}/data-model`

## Acceptance Criteria

- [ ] Clicking Add Rule from the scenario rules list creates a backend rule and navigates to the dedicated rule page.
- [ ] The dedicated rule page loads the rule and all authoring context correctly.
- [ ] Saving a rule sends backend-compatible AST, not a UI-only formula object.
- [ ] Editing a saved rule can restore supported AST into the simple visual builder.
- [ ] Unsupported AST shapes are detected and surfaced clearly.
- [ ] Validation results are visible and tied to the current rule.
- [ ] Rule groups are reusable and consistent.
- [ ] The app no longer has two conflicting rule authoring experiences.

## Nice-To-Have Later

- [ ] AI rule explanation
- [ ] AI formula generation
- [ ] Duplicate rule action from detail page
- [ ] Duplicate rule action from list page
- [ ] Advanced AST tree editor
- [ ] JSON preview toggle
- [ ] Autosave draft experiments after stable manual save flow is complete
