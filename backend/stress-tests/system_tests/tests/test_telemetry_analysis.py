import json
import unittest
from pathlib import Path
from tempfile import TemporaryDirectory

from system_tests.telemetry_analysis import summarize


def sample(elapsed, user, idle, commits, boot=1, reset="one", complete=True):
    return {"elapsed_seconds": elapsed, "complete": complete,
        "local": {"boot_time_epoch_seconds": boot, "measurements": {
            "cpu_seconds": {"user": user, "system": 0, "idle": idle}, "process_memory_bytes": {"rss": 4096}}},
        "database": {"metrics": {"database": {"xact_commit": commits, "stats_reset": reset}}}}


class TelemetryTests(unittest.TestCase):
    def summarize(self, values):
        with TemporaryDirectory() as directory:
            path = Path(directory) / "metrics"
            path.write_text("".join(json.dumps(v) + "\n" for v in values))
            return summarize(path)

    def test_rates_and_cpu_fraction_use_deltas(self):
        result = self.summarize([sample(0, 100, 100, 100), sample(10, 105, 115, 120)])
        self.assertEqual(result["observer_host_average_busy_fraction"], 0.25)
        self.assertEqual(result["database_commits_per_second"], 2)

    def test_restarts_are_excluded_and_not_negative_work(self):
        result = self.summarize([sample(0, 100, 100, 100), sample(10, 1, 1, 1, boot=2, reset="two")])
        self.assertIsNone(result["observer_host_average_busy_fraction"])
        self.assertIsNone(result["database_commits_per_second"])
        self.assertEqual(result["reset_intervals"], 2)

    def test_missing_samples_do_not_supply_zero_utilization(self):
        result = self.summarize([sample(0, 1, 1, 1), sample(10, 1, 1, 1, complete=False)])
        self.assertIsNone(result["observer_host_average_busy_fraction"])
        self.assertIsNone(result["database_commits_per_second"])
        self.assertEqual(result["invalid_component_intervals"], 1)
