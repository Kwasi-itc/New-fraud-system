# Understanding the system performance test

## Is the test fully done?

**The core discovery runners are implemented and have passed small live tests. The full
system performance and resilience test plan is not complete.**

At the time of writing, 141 automated tests passed. Separately, actual services
were tested against their own disposable shared PostgreSQL database. Ingestion,
sync decisions, async decisions and a stopped/restarted decision worker all passed
their small smoke tests. These results establish basic behavior, not maximum capacity.

| Area | Current status |
|---|---|
| Fixed-arrival ingestion, sync and async runners | Implemented and smoke-tested |
| Steady, burst and recovery traffic stages | Implemented |
| Completion deadlines, response identity checks and partial reports | Implemented |
| Optional existing host/service/PostgreSQL observation | Integrated, with coverage limits |
| Live validation against a prepared test tenant | Passed in a disposable synthetic tenant |
| Recovery checks for explicitly selected River queues | Implemented; SQL fixture and actual worker-recovery smoke passed |
| Full queue stability and recovery qualification | Not completed |
| All-queue coverage and downstream delivery verification | Not implemented |
| Worker stop/restart | Implemented with a restoration watchdog; live smoke passed |
| Dependency/network outage experiments | Not implemented |
| Independent verification of persisted business effects | Counts checked in isolated pilot; general identity-level checker still needed |
| Hardware sizing and production capacity qualification | Not completed |

Use the [queue campaign guide](QUEUE_CAMPAIGN.md) for commands and the
[full test plan](SYSTEM_PERFORMANCE_TEST_PLAN.md) for the broader roadmap.

## 1. Start with what the fraud system does

Imagine a customer makes a payment. Your system receives a transaction such as:

> Account A sent GH₵500 to Account B at 10:30.

Your system needs to store that transaction and evaluate it for fraud. The
performance test asks whether it can keep doing both quickly and reliably when
many transactions arrive together.

For the path being tested, the transaction follows this journey:

```text
Incoming transaction
        ↓
Ingestion service stores it
        ↓
Decision service receives a request to evaluate it
        ↓
The evaluation waits for an available worker
        ↓
The worker runs the fraud scenarios
        ↓
A completed decision becomes available
```

Each step takes time and uses resources. Ingestion writes to PostgreSQL. A
decision worker may read transaction history, evaluate rules and save results.
The services and workers share the same logical database, so they can compete
for database connections, CPU and disk access.

A slow transaction could be waiting for ingestion, waiting in a queue, running
expensive rules or waiting for the database. The test measures the overall
journey and collects clues about where pressure appears. It does not yet directly
measure every internal waiting period separately.

## 2. Why sending one transaction is not enough

Suppose you send one transaction manually and it finishes in half a second.
That tells us the path works for that transaction under those conditions.

It does not tell us what happens when:

- 100 transactions arrive together.
- New transactions arrive faster than workers finish them.
- A sudden burst ends, but hundreds of transactions are still waiting.
- Ingestion and decisions compete for the same database.

Think of a shop with one cashier. Serving one customer quickly does not tell you
whether the shop can handle a busload of customers. In the fraud system, the
waiting customers are queued work, and the cashiers are workers.

## 3. Why the queue matters

A queue lets the system accept work before a worker is ready to process it.
That is useful during a brief burst. But accepting work and finishing work are
different things.

For example:

| Every second | Transactions |
|---|---:|
| New work arrives | 30 |
| Workers finish | 20 |
| Additional work left waiting | 10 |

If those rates continue for a minute, approximately 600 more transactions are
waiting. The API might still respond quickly with “accepted,” while actual
decisions take longer and longer to arrive.

That is why the runner does not stop when the decision API accepts a request.
It waits for evidence that the decision finished.

## 4. What the script actually does

The script acts like a controlled transaction sender. You give it:

- A prepared test tenant.
- Transaction records matching that tenant's data model.
- Running ingestion and decision services, including the decision worker.
- A traffic schedule.
- A completion deadline.

For each scheduled transaction, it performs these steps:

1. **Send the transaction to ingestion.** It waits for a successful ingestion
   response. If ingestion fails or times out, it records the problem and does
   not proceed to decision submission for that transaction.
2. **Ask the decision service to process it asynchronously.** This means the
   service can accept work now and complete it later through a worker.
3. **Check for completion.** The runner periodically asks for the execution's
   status until it finishes, fails or reaches the observation timeout.
4. **Validate and record the result.** It checks transaction/execution identity,
   tenant identity where present in the contract, and the expected scenario
   result count. It records how long the journey took.

The test script explicitly sends the decision request after ingestion succeeds.
It measures that two-step path. It does not establish that ingestion automatically
triggers the same decision path through another integration.

These checks validate completion evidence and response structure. They do not
independently prove every fraud rule returned the correct business outcome or
that every downstream side effect was persisted exactly as intended.

## 5. Why we control the arrival rate

Suppose we want to test 10 transactions per second. The runner schedules a new
transaction approximately every 0.1 seconds, while earlier transactions may
still be processing.

Real incoming traffic does not necessarily slow down because workers are busy.
If the runner waited for each transaction to finish before sending another, a
slowing system would receive less work. That could hide overload.

The runner also limits how many transactions it tracks simultaneously. If that
limit fills up, or the runner is too late for a scheduled arrival, it records a
**generator drop**: a transaction it intended to send but could not submit.

That tells us the requested workload was not fully delivered. We cannot treat
that run as successfully handling the requested rate. The limit includes
ingestion, decision submission and completion polling.

## 6. Walking through the example campaign

These numbers are starting points for an experiment, not established system
capacity or production requirements.

| Stage | Schedule | Transactions |
|---|---|---:|
| Baseline | 10 per second for 60 seconds | 600 |
| Burst | 30 per second for 20 seconds | 600 |
| Recovery | 10 per second for 120 seconds | 1,200 |
| Total | 200 seconds of scheduled traffic | 2,400 |

### Baseline: normal traffic

We start at 10 transactions per second. We observe whether transactions complete
comfortably while traffic continues. Does outstanding work remain small? Are
completion times reasonably consistent?

### Burst: a sudden increase

We increase the rate to 30 per second. This represents many payments arriving
at once. We observe whether completion times rise and outstanding work grows.

A queue growing briefly is not automatically a failure. What matters is how
much waiting it creates and whether completion deadlines are met.

### Recovery: normal traffic continues

We return to 10 per second. We keep sending normal traffic because customers
usually do not stop transacting while the system clears a backlog.

We observe whether the burst transactions finish and later transactions continue
completing. The current runner can show this for its own transactions. It does
not automatically prove that every database queue returned to a healthy state.
The optional named-queue check also watches selected River queues and requires
their backlog and waiting age to stay within declared limits for a specified
observation window. That check must be explicitly configured; merely naming a
traffic stage “recovery” does not enable it. Live validation of the new database
collector is still pending.

After scheduled traffic ends, the runner waits for remaining operations within
their individual timeouts. Actual run duration can therefore exceed 200 seconds.

## 7. What the completion deadline means

With a five-second deadline, the question is:

> Did this transaction produce a verified completed decision within five
> seconds of when it was supposed to arrive?

That includes ingestion, decision submission, waiting and completion observation.
There is also a separate timeout controlling how long the script watches each
submitted operation.

For example:

- Completion deadline: 5 seconds.
- Observation timeout: 60 seconds.
- Decision finishes after 12 seconds.

The transaction finished successfully but missed its five-second deadline.
“Eventually finished” may be insufficient for a payment flow needing a timely
fraud result. Polling adds observation delay, so the measured time is when the
runner observes completion, not an exact server-internal completion timestamp.

## 8. How to read the results

| Result | Simple meaning |
|---|---|
| Offered | Arrivals the runner processed from the schedule; a completed schedule should reach the planned total |
| Submitted | Transactions the runner actually started sending |
| Ingested | Transactions with successful ingestion responses |
| Async accepted | Decision requests accepted for asynchronous processing |
| Completed | Transactions with verified completed decision results |
| Deadline met | Transactions completed within the required time |
| Generator dropped | Scheduled arrivals the runner could not submit |
| Unresolved | Transactions whose final outcome could not be established |

Here is an illustrative result, not a measurement of your system:

> We offered 2,400 transactions. All were submitted and ingested. All eventually
> completed, but 300 took longer than five seconds.

The path completed the observed work but missed the five-second requirement for
some transactions. We would investigate when those delays appeared and which
resources were under pressure.

Another example:

> We offered 2,400 transactions, but submitted only 1,800 because the runner's
> in-flight limit filled up.

That does not demonstrate support for the requested traffic. We would inspect
whether the runner's limit was too small, the system was too slow, or both.

A timeout does not prove a transaction was rejected or lost: the server might
have committed work before the response failed. The runner marks ambiguous
outcomes unresolved and does not automatically resubmit them.

A fraud result saying “decline this payment” is not a test failure. A valid,
completed fraud decline is successful technical processing.

The current response-level gate requires every planned transaction to complete
by its deadline, identity reconciliation to pass, and any requested telemetry
to be complete. Passing this gate is narrower than proving production readiness.

## 9. How this relates to hardware and performance

| Resource | How it relates to the system |
|---|---|
| CPU | Evaluating rules and processing requests |
| Memory | Holding active work, application state and caches |
| Database connections | Letting APIs and workers perform database operations |
| Disk | Persisting transactions, decisions and queue records |
| Network | Carrying requests between the runner, services and database |
| Workers | Executing queued decision work |

Optional resource measurements help answer:

> When completion became slower, what else changed?

For example, adding decision workers might improve completion speed. It might
also increase database pressure enough to slow ingestion. Because services share
PostgreSQL, improving worker throughput alone is insufficient if the overall
transaction journey becomes worse.

To investigate, repeat an equivalent workload, change one setting and compare
the results. Correlation alone does not prove the bottleneck; the controlled
change helps test the explanation.

Current host measurements describe the machine running the observer. They do
not automatically describe remote service hardware. PostgreSQL observation
provides raw counters and capped samples of some River jobs, not comprehensive
queue coverage. Missing data must not be interpreted as zero load.

Campaign reports also turn valid counter differences into CPU usage and database
commit rates. They discard intervals affected by resets or missing observations.
The CPU figure still describes the observer machine, and database commits can
include other work besides the test.

## 10. How this differs from the database-scale test

The existing `database_scale_suite.py` asks:

> Does ingestion and decision performance change when the database contains
> much more history?

A historical rule may behave differently against a small database and against
millions of transactions.

The new queue campaign asks:

> What happens when transactions arrive at a specified pace, including a burst?

These questions complement each other:

- Database-scale testing measures sensitivity to stored data volume.
- The queue campaign measures sensitivity to incoming traffic and asynchronous
  completion.

Eventually, a representative arrival schedule should be tested against a
representative amount of historical data. Keep the workload and initial data
comparable when evaluating different configurations.

## 11. What comes next

The small live tests have now run. Across the isolated service pilot, all 54
transactions were stored, with matching audit and outbox counts; all 34 expected
async executions completed. During the worker outage, the selected queue reached
7 active jobs and met its configured recovery limits afterward. This shows that
this small amount of work survived the controlled outage. It does not show how
the system behaves with thousands of transactions or a long database outage.

Next, run representative data at increasing rates using agreed normal/peak traffic,
decision deadlines and queue recovery limits. Repeat against realistic history and
the intended server configuration. Additional downstream paths and failure types
still need implementation. See [VALIDATION.md](VALIDATION.md) for the exact boundary
and [README.md](README.md) for repeatable commands.

The practical purpose is to replace guesses such as “this server should be
enough” with evidence such as:

> Under this transaction workload, with this history and configuration, the
> system completed the tested traffic within our deadline. The reports show
> what happened during the burst and afterward.

That conclusion requires a real run and recorded evidence. The implementation
and passing local harness tests alone do not establish it.
