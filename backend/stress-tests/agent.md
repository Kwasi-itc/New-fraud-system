# Stress Tests Agent Context

## Canonical Location

This folder is the canonical location for all stress tests:

```text
New-fraud-system\backend\stress-tests
```

Run commands from the API workspace root unless stated otherwise:

```powershell
cd C:\Users\kadu-\Desktop\ITC\fraud_system\api
```

## Systems Under Test

Two systems are compared:

- ITC/new fraud system
  - data model service
  - ingestion service
  - decision engine service
- Marble system
  - Marble API/backend
  - Marble worker for jobs/publication prep where needed
  - Firebase auth emulator for admin login in tests that create scenarios/rules

Marble has been run from WSL. Last known Marble API URL:

```powershell
$env:MARBLE_API_URL = "http://172.23.146.73:8080"
```

The WSL IP can change after restart. Use the latest URL printed by the server or latest run summaries if unsure.

## Environment Variables

### Marble Public/API-Key Tests

The active Marble scenario-creation tests generate a fresh `API_CLIENT` key after admin login. This avoids stale public API keys after deleting Marble containers/volumes. Set the API URL:

```powershell
$env:MARBLE_API_URL = "http://172.23.146.73:8080"
```

Optional if using a CLI flag instead:

```powershell
--api-url "http://172.23.146.73:8080"
```

### Marble Admin/Scenario-Creation Tests

Required when a Marble test creates data models, scenarios, or rules:

```powershell
$env:MARBLE_ADMIN_EMAIL = "jbe@zorg.com"
$env:MARBLE_ADMIN_PASSWORD = "very-secret"
$env:FIREBASE_AUTH_URL = "http://127.0.0.1:9099"
$env:FIREBASE_API_KEY = "dummy"
```

If Firebase emulator is running inside WSL and Windows cannot reach it on localhost, use the WSL IP:

```powershell
$env:FIREBASE_AUTH_URL = "http://172.23.146.73:9099"
```

You can also bypass Firebase login if you already have a Marble admin token:

```powershell
$env:MARBLE_ADMIN_TOKEN = "<token>"
```

### ITC/New-System Tests

Defaults usually assume:

```powershell
$env:DATA_MODEL_URL = "http://127.0.0.1:8080"
$env:INGESTION_URL = "http://127.0.0.1:8081"
$env:DECISION_ENGINE_URL = "http://127.0.0.1:8082"
```

If auth is enabled:

```powershell
$env:SERVICE_AUTH_TOKEN = "<token>"
```

If ingestion uses a separate Postgres database, many ITC tests need:

```powershell
--ingestion-database-url "postgres://fraud:fraud@localhost:5432/ingestion?sslmode=disable"
```

This prevents errors like:

```text
relation "tenant_<id>.<table>" does not exist
batch request exceeds maximum supported size
```

## Test Methodologies

### Current Domain Workload

Current active closed-loop/scaling tests use company-shaped transaction objects instead of generic stress fields:

```text
account_ref, processor, merchant_id, product_id, transaction_id, date, amount, currency, country, channel
```

Each trial seeds three accounts, three merchants, and three products. Transactions link by `account_ref`, `merchant_id`, and `product_id`. `processor` is randomly selected from `genpay,uniwallet`; `channel` is randomly selected from `card,wallet,bank`. Transaction `date` is monotonic and advances by a random interval between one hour and two days.

Current shared comparison variants:

```text
baseline_payload,nested_payload,custom_list_related_fuzzy,related_field,aggregate_count,aggregate_velocity,mixed_heavy
```

`custom_list_related_fuzzy` fuzzy-matches related merchant names against a merchant blacklist. `aggregate_count` counts seeded transactions for a merchant. `aggregate_velocity` sums merchant transaction amount over the velocity window, currently seven days.

Deprecated and not for new work unless explicitly requested:

```text
decision_throughput_limit.py
decision_throughput_orchestrator.py
marble_decision_throughput_limit.py
marble_decision_throughput_orchestrator.py
```

### Constant Arrival Rate

Scripts:

- `decision_throughput_limit.py`
- `marble_decision_throughput_limit.py`
- orchestrators wrap these scripts

These require a target `--rate`. They schedule request slots at that rate and drop locally if the configured VUs are saturated. Use these for “can the system sustain X EPS?”.

### Closed Loop VUs

Scripts:

- `decision_closed_loop_vus.py`
- `marble_decision_closed_loop_vus.py`
- scenario/rule/data-volume scaling scripts use closed-loop logic internally

These do not take `--rate`. They keep `--vus` active for the full duration. When one request completes, that VU immediately starts another. Use these for “what throughput does the system achieve at N concurrent active sessions?”.

### Scenario Scaling

Scripts:

- `decision_scenario_scaling.py`
- `marble_decision_scenario_scaling.py`

Varies:

- number of live scenarios
- rules per scenario
- rule complexity

Primary comparison matrix:

```powershell
--scenario-counts 1,5,10 --rules-per-scenario 1,5,10 --complexities baseline_payload,mixed_heavy --vus 5 --duration 60
```

### Rule Complexity Scaling

Scripts:

- `decision_rule_complexity_scaling.py`
- `marble_decision_rule_complexity_scaling.py`

Compares different rule formulas at the same VU levels.

For fair Marble vs ITC comparison, use only shared variants:

```text
baseline_payload,nested_payload,custom_list,related_field,aggregate_count_pushdown,mixed_heavy
```

Do not compare ITC default output to Marble default output without filtering, because ITC has extra variants:

- `decision_history`
- `related_records_count`

### Data Volume Sensitivity

Scripts:

- `decision_data_volume_sensitivity.py`
- `marble_decision_data_volume_sensitivity.py`

Varies seeded related-record volume, commonly:

```text
100,1000,10000,100000
```

Good variants:

```text
aggregate_count_pushdown,mixed_heavy
```

## Commands

### ITC Closed-Loop Threshold

```powershell
python .\New-fraud-system\backend\stress-tests\decision_closed_loop_vus.py --vus 5 --duration 60
```

### Marble Closed-Loop Threshold

Use the existing threshold scenario:

```powershell
python .\New-fraud-system\backend\stress-tests\marble_decision_closed_loop_vus.py --scenario-id 019ecba0-19d9-74c2-897c-f9e3ff7ca554 --vus 5 --duration 60
```

The scenario `019ecba0-19d9-74c2-897c-f9e3ff7ca554` triggers on all transactions and has one threshold rule: score `+10` if `transactions.value > 100`.

### ITC Constant-Rate Throughput

```powershell
python .\New-fraud-system\backend\stress-tests\decision_throughput_limit.py --rate 100 --vus 5 --duration 60
```

### Marble Constant-Rate Throughput

```powershell
python .\New-fraud-system\backend\stress-tests\marble_decision_throughput_limit.py --scenario-id 019ecba0-19d9-74c2-897c-f9e3ff7ca554 --rate 100 --vus 5 --duration 60
```

### ITC Throughput Orchestrator

```powershell
python .\New-fraud-system\backend\stress-tests\decision_throughput_orchestrator.py --vus 5 --duration 60
```

### Marble Throughput Orchestrator

```powershell
python .\New-fraud-system\backend\stress-tests\marble_decision_throughput_orchestrator.py --scenario-id 019ecba0-19d9-74c2-897c-f9e3ff7ca554
```

### ITC Scenario Scaling

```powershell
python .\New-fraud-system\backend\stress-tests\decision_scenario_scaling.py --scenario-counts 1,5,10 --rules-per-scenario 1,5,10 --complexities baseline_payload,mixed_heavy --vus 5 --duration 60 --ingestion-database-url "postgres://fraud:fraud@localhost:5432/ingestion?sslmode=disable"
```

### Marble Scenario Scaling

```powershell
python .\New-fraud-system\backend\stress-tests\marble_decision_scenario_scaling.py --scenario-counts 1,5,10 --rules-per-scenario 1,5,10 --complexities baseline_payload,mixed_heavy --vus 5 --duration 60
```

### ITC Rule Complexity

Fair comparison variants:

```powershell
python .\New-fraud-system\backend\stress-tests\decision_rule_complexity_scaling.py --variants baseline_payload,nested_payload,custom_list,related_field,aggregate_count_pushdown,mixed_heavy --vus 5,10,30 --duration 60 --ingestion-database-url "postgres://fraud:fraud@localhost:5432/ingestion?sslmode=disable"
```

Baseline/threshold only:

```powershell
python .\New-fraud-system\backend\stress-tests\decision_rule_complexity_scaling.py --variants baseline_payload --vus 5,10,30 --duration 60 --ingestion-database-url "postgres://fraud:fraud@localhost:5432/ingestion?sslmode=disable"
```

### Marble Rule Complexity

Fair comparison variants:

```powershell
python .\New-fraud-system\backend\stress-tests\marble_decision_rule_complexity_scaling.py --variants baseline_payload,nested_payload,custom_list,related_field,aggregate_count_pushdown,mixed_heavy --vus 5,10,30 --duration 60
```

Baseline/threshold only:

```powershell
python .\New-fraud-system\backend\stress-tests\marble_decision_rule_complexity_scaling.py --variants baseline_payload --vus 5,10,30 --duration 60
```

### ITC Data Volume

```powershell
python .\New-fraud-system\backend\stress-tests\decision_data_volume_sensitivity.py --data-sizes 100,1000,10000,100000 --variants aggregate_count_pushdown,mixed_heavy --vus 5 --duration 60 --ingestion-database-url "postgres://fraud:fraud@localhost:5432/ingestion?sslmode=disable"
```

### Marble Data Volume

```powershell
python .\New-fraud-system\backend\stress-tests\marble_decision_data_volume_sensitivity.py --data-sizes 100,1000,10000,100000 --variants aggregate_count_pushdown,mixed_heavy --vus 5 --duration 60
```

## Result Folders

Common output folders:

- `throughput-runs`
- `marble-throughput-runs`
- `scenario-scaling-runs`
- `marble-scenario-scaling-runs`
- `rule-complexity-runs`
- `marble-rule-complexity-runs`
- `data-volume-runs`
- `marble-data-volume-runs`

Each timestamped run generally contains:

- `summary.json`
- one or more per-trial JSON files

Use the latest timestamped folder when comparing recent results.

## Known Comparison Findings

### Scenario Scaling

Earlier, Marble appeared better in mixed-heavy rule/scenario fanout. Root causes:

- ITC mixed-heavy originally included decision-history lookup.
- ITC aggregate originally used filtered owner/status aggregate.
- Marble mixed-heavy did not include decision-history.
- Marble aggregate was unfiltered.

Load was equalized:

- ITC `aggregate_count_pushdown` now uses unfiltered aggregate.
- ITC `mixed_heavy` now excludes decision-history.
- Shared mixed-heavy shape is:
  - nested payload
  - custom list
  - related field
  - aggregate count

ITC then still lagged in repeated mixed-heavy fanout because it recomputed repeated expensive AST dependencies. An optimization was implemented in `New-fraud-system/backend/decision-engine-service`:

- request/scenario-scoped `EvaluationCache`
- cache integrated into `asteval.Runtime`
- `EvaluateNode` uses cache for non-constant nodes
- `/decisions/all` shares one cache across the scenario loop
- singleflight avoids concurrent duplicate computation

After rebuild, ITC improved and beats Marble in most mixed-heavy scenario-scaling cases.

### Rule Complexity

For fair rule-complexity comparisons, restrict ITC to the six Marble-supported variants. ITC default includes additional variants that Marble does not support:

- `decision_history`
- `related_records_count`

## Troubleshooting

### Marble 403 When Creating Scenarios

If Marble admin setup fails with:

```text
missing permission SCENARIO_CREATE
```

Use a real admin login token through Firebase emulator or create the scenario manually and pass its ID to public/API-key tests.

### Marble Publication Prep Timeout

If publishing aggregate scenarios fails with:

```text
scenario publication preparation did not complete within 120s
```

Ensure the Marble worker is running and processing index/data-preparation jobs. For tests, aggregate formulas were adjusted to unfiltered aggregate to avoid requiring publication index preparation.

### Batch Too Large

Marble and ITC ingestion endpoints have batch-size limits. Current scripts batch large seed sets:

- Marble related records: batch size 100
- ITC related records: batch size 500

If `too many objects in the batch` appears, check batching logic before rerunning.

### WSL / Windows Networking

If Windows Python cannot reach WSL services:

- use the WSL service IP, for example `172.23.146.73`
- avoid malformed URLs such as `http://`
- verify:

```powershell
Invoke-RestMethod "$env:MARBLE_API_URL/v1/-/version"
```

### Firebase Emulator

Firebase CLI did not accept `--host` in one prior run. Use the supported emulator config or run it so Windows can reach it at either:

```powershell
http://127.0.0.1:9099
```

or:

```powershell
http://172.23.146.73:9099
```

## Reporting Format

Preferred comparison table columns:

```text
Complexity
Scenarios x Rules
Marble RPS
ITC RPS
Marble slowdown
ITC slowdown
Throughput penalty
```

Slowdown should be relative to each system's own `Baseline 1 x 1`.

Throughput penalty direction should make the slower system worse:

- `6.48x Marble worse`
- `2.05x ITC worse`

For RPS, lower is worse. For latency, higher is worse.
