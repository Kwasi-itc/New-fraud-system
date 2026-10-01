# System tests

System performance and resilience campaigns live here. Run the following commands
from `backend/stress-tests` so both Python packages are importable.

| File | Purpose |
|---|---|
| [SYSTEM_PERFORMANCE_TEST_PLAN.md](SYSTEM_PERFORMANCE_TEST_PLAN.md) | Full campaign plan and implementation status |
| [QUEUE_CAMPAIGN.md](QUEUE_CAMPAIGN.md) | Queue experiment prerequisites, commands, metrics and limitations |
| [queue_campaign.py](queue_campaign.py) | Fixed-arrival async-decision runner with steady/burst/recovery stages |
| [tests/test_queue_campaign.py](tests/test_queue_campaign.py) | Local fake-client tests; no running services required |

Install dependencies and inspect the runner:

```shell
python -m pip install -r system_tests/requirements.txt
python -m system_tests.queue_campaign --help
```

Run system harness correctness tests:

```shell
python -m unittest discover -s system_tests/tests -t .
```

Run both the shared replay and system harness tests:

```shell
python -m unittest discover -s production_replay/tests -t .
python -m unittest discover -s system_tests/tests -t .
```

Use `system_tests/runs/<unique-run-name>` for generated artifacts; `runs/` is
ignored by Git. The campaign reuses API clients, completion verification,
reporting, identity tracking and observation utilities from `production_replay`.
The existing database-scale runner and its documentation remain in that package.

Only the initial async-decision campaign is implemented. See the plan and queue
guide for remaining fault, queue-coverage and hardware-qualification work.
