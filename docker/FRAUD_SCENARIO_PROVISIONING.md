# EC2 fraud scenario provisioning

`provision-fraud-scenarios-ec2.mjs` imports the supplied portable fraud data model when the target tenant has none, or verifies an exact match when one already exists. It then creates or reconciles Decision Engine scenarios through the EC2 gateway. It is separate from the database/root-tenant setup scripts and never chooses a tenant automatically.

The gateway must route `/services/data-model` and `/services/decisions` to their corresponding services. The target tenant must already exist and be active. Provide the gateway's reachable `ip:port` and its tenant UUID explicitly.

## Run

Set the service bearer tokens in the environment without adding them to command-line arguments:

```sh
export DATA_MODEL_SERVICE_TOKEN='...'
export DECISION_ENGINE_SERVICE_TOKEN='...'
node docker/provision-fraud-scenarios-ec2.mjs \
  --host 10.0.0.10:8080 \
  --tenant-id 00000000-0000-4000-8000-000000000000
```

An HTTPS origin is also accepted, for example `--host https://fraud.example:8080`. For private-network HTTP deployments, keep the gateway and machine running this script on a trusted network; bearer tokens are not encrypted over HTTP.

The model file defaults to `fraud-data-model.json` at the repository root. Override it with `--model-file`. `--late-callback-seconds` controls the Family E late-callback threshold and defaults to `300`. D7's near-limit rule defaults to a limit of `1000` and a lower-band width of `10%`; override these with `--structuring-limit` and `--structuring-band-percent`. For example, the default band is `900 <= amount < 1000`.

Run `--dry-run` first to validate the local model and display the rules that remain unsupported without making network requests.

## Safety and verification

Before any scenario writes, the script verifies that the tenant is active and checks the existing exported model. If the tenant model is empty, it imports the supplied portable model, checks the import summary, and exports it again to verify an exact match. Existing matching models are left unchanged. A partial or different existing model stops the run rather than attempting an unsafe merge. The import endpoint is best-effort across multiple operations and is not globally transactional; if import reports an error or post-import verification fails, inspect the tenant model before retrying.

Scenarios are matched by name and rules by stable rule ID. Existing scenarios with all desired rules are left unchanged. If supported rules are missing, the script reuses a compatible draft or creates one from the current live version, adds only missing rules, validates and commits the iteration, publishes it, then verifies the live iteration and publication history. Conflicting/duplicate stable IDs, ambiguous scenario names, or an incompatible draft stop provisioning rather than overwriting unknown configuration. If a later stage fails, earlier scenario versions may already have published; inspect service state before retrying.

Each created or changed iteration is read back and checked, validated, committed, prepared if required, published, and verified before the script proceeds. A successful dry run only validates local input and definitions; it does not exercise service APIs or prove that production query plans are acceptable.

## Rule scope and assumptions

The script provisions five scenarios and all rules supported by the current Aggregator pushdown, including:

- **A1, A2, B5, D1, D3, D4, D7, and E5:** scalar aggregate counts/count-distinct with model-supported predicates and time filters.
- **A4:** destination credit volume in the prior 24 hours compared with five times the average daily volume over the preceding 30 days, excluding the current 24-hour window. The daily average is calculated as the historical 30-day sum divided by 30; the rule does not fire when that historical volume is zero. This replaces the prior median-based proposal.
- **D7:** counts debit transactions by user in the prior 24 hours whose amount is in the configured band immediately below the limit. The defaults are `900 <= amount < 1000`.
- **E5:** the current transaction is included when counting undetermined transactions for the same user in the prior 24 hours.
- **D3:** counts failed debits for the same wallet earlier than the current transaction across all available history. The supplied rule definition has no explicit lookback/reset period, so this is deliberately not capped to a recent window.

Only these rules remain omitted:

- **A5:** requires device-sharing comparisons, but the supplied model has no device identifier and the rule is cross-record/pairwise.
- **D2:** requires an ordered descending sequence; current aggregate pushdown returns scalar aggregates and has no ordering/window-function operation.

The rules are syntactically and model-field checked locally, but no live EC2 API validation, representative tenant-volume query-plan assessment, index assessment, or performance test has been performed here. Aggregator pushdown runs database-side queries, but pushdown alone does not establish acceptable cost. Treat the rules' weights, thresholds, assumptions, and operational usefulness as uncalibrated until reviewed against representative data.

Family E rules 1 and 3 use the configured late-callback threshold; rule 2 retains its 900-second threshold.

## Tests

Run the standalone Node tests with:

```sh
node --test docker/provision-fraud-scenarios-ec2.test.mjs
```
