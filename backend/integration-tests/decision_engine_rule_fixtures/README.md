# Decision Engine Rule Fixtures

Each JSON file defines one black-box rule evaluation contract for the decision-engine service. The harness auto-discovers every `*.json` file under this folder recursively, so a normal new fixture does not need to be registered in Python code.

Run the fixture suite from the backend root:

```bash
python -m pytest integration-tests/test_decision_engine_rule_fixtures.py -q
```

Run the full integration suite:

```bash
python -m pytest
```

## Fixture Flow

For a successful fixture, the harness:

1. Creates or reuses a dedicated integration tenant model for rule fixtures.
2. Resolves placeholders.
3. Seeds records and generated record batches.
4. Creates a scenario, iteration, and rules.
5. Validates, commits, and publishes the iteration.
6. Evaluates one input payload.
7. Asserts trigger result, decision score/outcome, per-rule outcomes, persisted decision lookup, and scenario decision listing.

Invalid fixtures stop after validation and assert that the invalid rule set does not publish.

## Placeholders

The harness resolves these placeholders before running a case:

- `$transactions`: the generated integration transaction table name
- `$accounts`: the generated integration account table name
- `$now`: current UTC timestamp
- `$now_minus_30s`: current UTC timestamp minus 30 seconds
- `$now_minus_2m`: current UTC timestamp minus 2 minutes
- `$now_minus_4m`: current UTC timestamp minus 4 minutes
- `$now_minus_10m`: current UTC timestamp minus 10 minutes
- variables declared under `variables`, such as `$object_id` or `$owner_id`

Generated records also support index tokens inside string fields:

- `$index`
- `$index_mod_2`
- `$index_mod_3`
- `$index_mod_4`
- `$index_mod_5`
- `$index_mod_10`

## Fixture Shape

Minimal successful fixture:

```json
{
  "name": "example high amount",
  "object_type": "$transactions",
  "trigger": {
    "function": "constant",
    "constant": true
  },
  "variables": {
    "object_id": "example_txn"
  },
  "input": {
    "object_id": "$object_id",
    "object_type": "$transactions",
    "fields": {
      "object_id": "$object_id",
      "amount": 1200,
      "status": "pending",
      "account_id": "acct-example",
      "ip": "1.2.3.4",
      "merchant": "Example Merchant",
      "email": "example@example.com",
      "country": "gh",
      "owner_id": "owner-example",
      "event_time": "$now",
      "note": null
    }
  },
  "rules": [
    {
      "name": "amount_over_1000",
      "score_modifier": 1,
      "formula": {
        "function": "gt",
        "children": [
          {
            "function": "field_ref",
            "named_children": {
              "field": {
                "constant": "amount"
              }
            }
          },
          {
            "constant": 1000
          }
        ]
      }
    }
  ],
  "expected": {
    "triggered": true,
    "score": 1,
    "outcome": "review",
    "rules": {
      "amount_over_1000": "hit"
    }
  }
}
```

Invalid validation fixture:

```json
{
  "expected": {
    "validation_valid": false,
    "validation_error_contains": "message fragment"
  }
}
```

Trigger miss fixture:

```json
{
  "expected": {
    "triggered": false
  }
}
```

## Seed Data

Use `records` for explicit single-record ingests:

```json
{
  "records": [
    {
      "object_type": "$transactions",
      "body": {
        "object_id": "seed_txn",
        "amount": 100,
        "status": "pending",
        "account_id": "acct-seed",
        "ip": "1.2.3.4",
        "merchant": "Seed Merchant",
        "email": "seed@example.com",
        "country": "gh",
        "owner_id": "owner-seed",
        "event_time": "$now_minus_30s",
        "note": "seed"
      }
    }
  ]
}
```

Use `generated_records` for velocity or aggregation cases. Generated batches are automatically split into chunks of 500 to match the ingestion API limit.

```json
{
  "generated_records": [
    {
      "object_type": "$transactions",
      "count": 501,
      "template": {
        "object_id": "velocity_$index",
        "amount": 25,
        "status": "pending",
        "account_id": "acct-$index_mod_3",
        "ip": "1.2.3.4",
        "merchant": "Velocity Merchant",
        "email": "velocity@example.com",
        "country": "gh",
        "owner_id": "owner-velocity",
        "event_time": "$now_minus_30s",
        "note": "recent"
      }
    }
  ]
}
```

## Current Coverage

Coverage is grouped by fixture folder:

- `core_ast`: comparisons, boolean logic, arithmetic, strings, lists, null and empty handling, trigger miss behavior
- `marble_compat`: `Payload`, `List`, fuzzy matching, string helpers, time helpers, templates, concatenation
- `related_data`: relationship traversal and Marble-style database access
- `aggregation`: related records, filters, sums, velocity windows, same-amount bursts, ratio checks, geographic churn, merchant fanout, test-charge escalation
- `decision_history`: prior decision counting
- `failure_modes`: invalid rules and validation failures

Prefer including both hit and no-hit rules in the same fixture when possible. The no-hit control is what proves the rule is filtering correctly instead of only proving that some broad condition can match.
