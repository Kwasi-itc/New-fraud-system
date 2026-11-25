# EC2 fraud scenario draft provisioning

`provision-fraud-scenarios-ec2.mjs` verifies the active tenant and its existing portable data model, then creates or reconciles Decision Engine scenario drafts through the gateway. It never imports or changes the model and never commits, prepares, or publishes scenario iterations. Existing live scenarios remain unchanged. The target tenant UUID must be supplied explicitly.

The gateway must route `/oauth/token`, `/services/data-model`, and `/services/decisions`. The target tenant and the matching `fraud-data-model.json` model must already be provisioned.

## Client setup

`platform-keys.mjs` provisions the platform issuer's signing and trust keys as part of auth-service setup. It does not create API-client key pairs. Generate a client-owned pair with `client-proof.mjs`:

```sh
node backend/auth-service/scripts/client-proof.mjs keygen \
  --dir .local/client-auth \
  --kid fraud-scenario-provisioner
```

Register the resulting `public.pem` and key ID on an API client in the target tenant. Grant exactly these normal client scopes:

- `models:read` to verify tenant status and the existing model
- `scenarios:read` to inspect scenarios and iterations
- `scenarios:write` to create scenarios, drafts, rules, and validate drafts

Do not grant `scenarios:publish`. The API keeps commit and publication operations human-only; the automation does not call them.

## Run

```sh
node docker/provision-fraud-scenarios-ec2.mjs \
  --host 10.0.0.10:8080 \
  --tenant-id 00000000-0000-4000-8000-000000000000 \
  --client-id 11111111-1111-4111-8111-111111111111 \
  --key-dir .local/client-auth
```

The token endpoint defaults to `<host>/oauth/token`; use `--token-url` if the public auth endpoint differs. A fresh client proof is created and exchanged for a scoped token in memory for each run. The script does not read or print service bearer tokens or the client access token. Keep `private.pem` private and use HTTPS outside trusted local development.

The model file defaults to `fraud-data-model.json` at the repository root. Override it with `--model-file`. `--late-callback-seconds` controls the Family E late-callback threshold and defaults to `300`. D7's near-limit rule defaults to a limit of `1000` and a lower-band width of `10%`; override these with `--structuring-limit` and `--structuring-band-percent`. For example, the default band is `900 <= amount < 1000`.

Run `--dry-run` first to validate the local model and display the rules that remain unsupported without making network requests.

## Safety and verification

Before any scenario writes, the script verifies that the tenant is active and checks the existing exported model. An empty or different model stops the run; model import/update remains a separate human-authorized setup operation. Scenario definitions are matched by name and rules by stable rule ID. If supported rules are missing, the script resumes one compatible draft or creates one from the current live/committed version, adds only missing rules, validates the iteration, and verifies that it remains a draft. Conflicting/duplicate stable IDs, ambiguous scenario names, incompatible drafts, or multiple drafts stop provisioning rather than overwriting unknown configuration.

Completed live scenarios are left unchanged. A compatible new or resumed draft is left unpublished for human review and publication. A failure can leave earlier draft edits in place; inspect service state before retrying. The script's draft-only guarantee is enforced both by its call sequence and by the API authorization boundary, not by relying on omission of a UI action.

After reviewing and committing a draft in the frontend, open **Publish**. If indexes are needed, select **Start or retry preparation** and wait for preparation to complete before selecting **Publish**. Preparation runs in the background, so the window can be closed and reopened to check progress.

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
node --test backend/auth-service/scripts/client-proof.test.mjs
```
