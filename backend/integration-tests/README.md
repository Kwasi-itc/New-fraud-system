# Integration Tests

Python integration tests for the data-model, ingestion, decision-engine, and screening services.

## Prerequisites

- Python 3.12+
- The dependencies from `requirements.txt`
- The services running and migrated

Default service URLs:

- `DATA_MODEL_URL=http://localhost:8080`
- `INGESTION_URL=http://localhost:8081`
- `DECISION_ENGINE_URL=http://localhost:8082`
- `SCREENING_URL=http://localhost:8085`

Override those environment variables when testing a different deployment.

## Run

From the backend root:

```bash
python -m pip install -r integration-tests/requirements.txt
python -m pytest
```

Or from this folder:

```bash
python -m pip install -r requirements.txt
python -m pytest
```

If you need to migrate the local services first:

```bash
cd ../data-model-service && go run ./cmd/migrate up
cd ../ingestion-service && go run ./cmd/migrate up
cd ../decision-engine-service && go run ./cmd/migrate up
cd ../screening-service && go run ./cmd/migrate up
cd ../integration-tests
python3 -m pip install -r requirements.txt
python3 -m pytest
```

Restart services after code changes and migrations. The suite waits for each service `/readyz` endpoint before running API tests.

The backend root has a `pytest.ini`, so `python -m pytest` works from `backend` without marker warnings and discovers `integration-tests` automatically.

For a detailed pass-only summary that includes each test purpose, endpoints called, status codes, and endpoint response snippets:

```bash
python3 -m pytest --endpoint-summary
```

By default, endpoint output is not truncated. Limit the response snippet size when the output is too long:

```bash
python3 -m pytest --endpoint-summary --endpoint-output-limit=300
```

Write only the detailed endpoint summary to a file:

```bash
python3 -m pytest --endpoint-summary-file=endpoint-summary.txt
```

You can combine terminal and file output:

```bash
python3 -m pytest --endpoint-summary --endpoint-summary-file=endpoint-summary.txt --endpoint-output-limit=300
```

If token auth is enabled for the services, set:

```bash
export SERVICE_AUTH_TOKEN=<token>
```

## Coverage Guard

`test_openapi_coverage.py` parses `data-model.yaml`, `ingestion.yaml`, `decision-engine.yaml`, and `screening.yaml`.
It fails when a documented HTTP endpoint is not represented in the integration-test coverage registry.

## Decision Rule Fixtures

The decision-engine rule contract suite is fixture-driven. Add new rule cases as `.json` files under:

```text
integration-tests/decision_engine_rule_fixtures/
```

The pytest harness auto-discovers every JSON fixture recursively, so no Python code change is needed for normal new cases. Change the harness only when a fixture needs a new placeholder, generated-data helper, assertion type, or setup phase.

Run just the fixture suite with:

```bash
python -m pytest integration-tests/test_decision_engine_rule_fixtures.py -q
```

Current fixture coverage includes:

- core AST operations: comparisons, boolean logic, arithmetic, strings, lists, null/empty handling, trigger miss behavior
- Marble compatibility helpers: `Payload`, `List`, `ContainsAnyOf`, `ContainsNoneOf`, `FuzzyMatch`, `FuzzyMatchAnyOf`, `TimeAdd`, `TimestampExtract`, `StringTemplate`, `StringConcat`
- related data access: `related_field`, `DatabaseAccess`, `related_records`, `filter_eq`, `map_field`, `list_count`, `sum`
- aggregation and velocity rules: high-volume one-minute checks, same-amount bursts, decline-rate ratios, geographic churn, merchant fanout, test-charge escalation
- decision history: `past_decision_count`
- validation failures: invalid/non-boolean rules that should not publish
