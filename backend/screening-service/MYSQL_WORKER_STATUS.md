# Screening MySQL worker status

Native work uses durable domain rows and fenced leases. PostgreSQL River jobs are not copied into this runtime. Checked acceptance requires complete passing evidence.

## Worker checklist

- [x] Counterparty whitelist suppression retains candidates and immutable application receipts; inline evidence preserves evaluation-time suppression.
- [x] Screening dispatch, recovery, match persistence and stale-claim fencing.
- [x] Monitored-object reads, continuous-run provenance and atomic pending screening creation.
- [x] Dataset catalog/delta/rescreen jobs, bounded checkpoint pages beyond 500 objects and fenced recovery.
- [x] Decision projection through the decision-owned reconciliation contract and retained source queue.
- [x] Case callback claims, retry/backoff, terminal failure and explicit replay with stable event identity.
- [x] Restricted screening worker healthy in local Docker.
- [x] Case evidence callback: authoritative publication, atomic import/hash verification, replay and real Docker worker delivery.
- [ ] Real provider acceptance, production worker capacity, process recovery and queue/cutover reconciliation.

Provider calls occur outside SQL transactions. Manual results are synthetic in the default development profile. Source error responses and unconfigured integrations must not be counted as successful functional coverage.

See [endpoint checklist](MYSQL_ENDPOINT_STATUS.md) and [migration plan](MYSQL_PLAN.md).
