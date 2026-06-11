# Fixture Demo Console

Local demo console for the fraud-system integration fixtures. It lets you pick a decision-engine fixture, make constrained edits, and run the real service flow stage by stage or all at once.

## Prerequisites

Start and migrate the backend services first:

```bash
make start-up
```

Default service URLs:

```bash
DATA_MODEL_URL=http://localhost:8080
INGESTION_URL=http://localhost:8081
DECISION_ENGINE_URL=http://localhost:8082
```

If token auth is enabled:

```bash
export SERVICE_AUTH_TOKEN=<token>
```

## Run

From the backend root:

```bash
python3 -m pip install -r ui_test/requirements.txt
python3 ui_test/server.py
```

Open:

```text
http://127.0.0.1:8099
```

Use another port:

```bash
UI_TEST_PORT=8100 python3 ui_test/server.py
```

## What It Does

- Loads fixtures from `integration-tests/decision_engine_rule_fixtures`.
- Creates a fresh tenant model per session.
- Applies constrained edits: input fields, thresholds, rule scores, generated counts, expected outcomes, rule constants, and compatible rule operators.
- Provides stage-specific inputs, including tenant and table naming controls.
- Shows non-editable stage values as disabled grey inputs.
- Runs these stages against the live services: tenant, tables, fields, records, scenario, rules, validation, publish, evaluation, history.
- Retrieves created state after each stage where the services expose read endpoints.
- Presents stage results as cards, tables, and badges instead of raw JSON.
- Keeps original fixture files unchanged.

## Rule Editing

The rule page renders each rule as editable AST nodes.

- Constant nodes can be edited without changing the fixture file.
- Operators can be swapped only inside compatible groups, such as `gt` to `gte`, `eq` to `neq`, or `and` to `or`.
- Field references and complex helper shapes remain read-only.
- The console does not add or remove rule conditions.

## Test

```bash
python3 -m pytest ui_test/tests
```
