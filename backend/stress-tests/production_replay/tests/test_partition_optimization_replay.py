from __future__ import annotations

import unittest
from datetime import datetime, timezone

from partition_optimization_replay import synthetic_records, triggered_rules


class PartitionOptimizationReplayTests(unittest.TestCase):
    def test_synthetic_records_cover_requested_days_without_source_files(self) -> None:
        fields = {
            "object_id", "transaction_id", "amount", "channel", "date", "direction",
            "stream_id", "system_type", "account_ref", "updated_at",
        }
        records = synthetic_records("run1", datetime(2026, 7, 1, tzinfo=timezone.utc), 3, 2, fields)
        self.assertEqual(len(records), 6)
        self.assertEqual(len({item["object_id"] for item in records}), 6)
        self.assertTrue(all(set(item) <= fields for item in records))
        self.assertTrue(all("updated_at" not in item for item in records))
        self.assertTrue(records[0]["date"].startswith("2026-07-01"))
        self.assertTrue(records[-1]["date"].startswith("2026-07-03"))

    def test_triggered_rules_only_returns_hit_executions(self) -> None:
        response = {"result": {"results": [{"rule_executions": [
            {"rule_name": "One Hour Transfer Burst", "outcome": "hit"},
            {"rule_name": "Weekly Transfer Velocity", "outcome": "no_hit"},
        ]}]}}
        self.assertEqual(triggered_rules(response), {"One Hour Transfer Burst"})


if __name__ == "__main__":
    unittest.main()
