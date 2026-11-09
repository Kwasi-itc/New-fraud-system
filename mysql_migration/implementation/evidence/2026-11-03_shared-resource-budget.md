# Shared database resource budget (D20.13)

This is the reviewed single-replica acceptance configuration, not a claim that
the current default deployment enforces these limits. All application services
use the same logical PostgreSQL database. Do not start acceptance with the
CPU-dependent/unbounded defaults. Recompute this budget before scaling replicas.

| Process | Replicas | Pools per replica | Maximum connections | Work concurrency |
|---|---:|---|---:|---|
| Auth API | 1 | application | 5 | pool bounded |
| Model API | 1 | application | 4 | pool bounded |
| Model worker | 1 | application | 4 | 2 index jobs |
| Ingestion API | 1 | write + optional read | 8 + 4 | 8 write, 4 read, 2 aggregate |
| Ingestion worker | 1 | worker | 4 | 2 workers |
| Decision API | 1 | application | 8 | 4 live decisions; rule/scenario/aggregate fanout 1 |
| Decision worker | 1 | application | 8 | 1 per each of 7 queues |
| Screening API | 1 | application | 4 | pool bounded |
| Screening worker | 1 | application | 4 | 1 per each of 3 queues |
| Case API | 1 | application | 4 | pool bounded |
| Case worker | 1 | application | 4 | 2 workers |
| Screening case delivery | 1 | application | 2 | 1 poller; batch 50 |
| Keycloak | 1 | identity | 10 | pool bounded |

Application maximum: **63** connections including the optional ingestion read
pool. Keycloak shares the server resource budget: **10**, total **73**. Against
PostgreSQL `max_connections=100`, reserve **27**: 3 superuser slots, 10 for
ordered migration/administrative work, and 14 operational headroom. Do not run
multiple migration commands concurrently with acceptance. Additional replicas,
read pools, tools and background schedulers must be counted before starting.

Enforcement inputs: `DATABASE_MAX_CONNS` for ingestion, decision and screening;
`READ_DATABASE_MAX_CONNS=4`, `WORKER_DATABASE_MAX_CONNS=4` for ingestion;
`DB_POOL_MAX_CONNS=4` for case; `KC_DB_POOL_MAX_SIZE=10` for Keycloak. The model
service uses pgx's DSN `pool_max_conns=4` (its pool constructor has no environment
override). Auth's server constructor explicitly sets 5. Use connection-string
query parameters through the secret/configuration workflow; do not add a second
application database. Every worker has its own pool and is counted separately.

Decision queue controls: SCHEDULED_EXECUTION_QUEUE_WORKERS,
ASYNC_EXECUTION_QUEUE_WORKERS, ASYNC_EXECUTION_CALLBACK_QUEUE_WORKERS,
WORKFLOW_DISPATCH_QUEUE_WORKERS, SCREENING_DISPATCH_QUEUE_WORKERS,
SCORING_DISPATCH_QUEUE_WORKERS and OUTBOX_QUEUE_WORKERS, all 1. Set
LIVE_DECISION_CONCURRENCY_LIMIT=4, RULE_EVALUATION_CONCURRENCY=1,
SCENARIO_EVALUATION_CONCURRENCY=1, AGGREGATE_REMOTE_CONCURRENCY_LIMIT=1.
Model INDEX_JOB_QUEUE_WORKERS=2; screening SCREENING_QUEUE_WORKERS,
DATASET_JOB_QUEUE_WORKERS and MONITORED_OBJECT_QUEUE_WORKERS all 1.

Inventory sources: root docker-compose.yml (one API and worker instance per
service); each service internal/app/config.go and pool constructor;
auth-service/cmd/server/main.go; decision-engine-service/cmd/worker/main.go.
Defaults currently include CPU-derived pgx pools, decision queue concurrency
26, screening queue concurrency 20 and optional ingestion read pools. Those
defaults are **not certified to fit** this budget.

Connection arithmetic is a capacity bound, not measured throughput. D14.12 and
D15.17 still require agreed load targets. During their acceptance runs collect
pool wait duration, active connections, CPU, I/O, buffer hit ratio, temporary
bytes, queue age and pg_stat_activity lock waits across all services. Keep
transactions short, prohibit network calls under locks, and run schema changes
in ordered maintenance windows with lock timeouts. Stop or reduce work on
resource exhaustion rather than increasing pools beyond the server budget.
Production replicas, failover headroom, workload CPU/I/O and latency acceptance
remain deployment/load gates; this plan does not activate MySQL or change the
shared PostgreSQL architecture.
