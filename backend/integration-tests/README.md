# Integration Tests

Python integration tests for the data-model, ingestion, and decision-engine services.

## Prerequisites

- Python 3.12+
- The dependencies from `requirements.txt`
- The three services running and migrated

Default service URLs:

- `DATA_MODEL_URL=http://localhost:8080`
- `INGESTION_URL=http://localhost:8081`
- `DECISION_ENGINE_URL=http://localhost:8082`

Override those environment variables when testing a different deployment.

## Run

```bash
cd ../data-model-service && go run ./cmd/migrate up
cd ../ingestion-service && go run ./cmd/migrate up
cd ../decision-engine-service && go run ./cmd/migrate up
cd ../integration-tests
python3 -m pip install -r requirements.txt
python3 -m pytest
```

Restart services after code changes and migrations. The suite waits for each service `/readyz` endpoint before running API tests.

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

`test_openapi_coverage.py` parses `data-model.yaml`, `ingestion.yaml`, and `decision-engine.yaml`.
It fails when a documented HTTP endpoint is not represented in the integration-test coverage registry.
