from __future__ import annotations

import json
import tempfile
import unittest
from pathlib import Path

from production_replay.manifest import load_manifest
from production_replay.scenarios import build_portable_scenarios
from production_replay.sorting import build_sorted_chunks, iter_merged_events
from production_replay.tests.helpers import manifest_data, stream, transaction_row, write_minimal_sources, write_transactions


class SortingAndScenarioTests(unittest.TestCase):
    def test_streams_are_globally_sorted_with_stable_equal_timestamp_order(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            write_minimal_sources(root)
            write_transactions(
                root / "inflow.csv",
                [
                    transaction_row(source_trans_id="late", source_date_created="2026-02-01 10:00:02"),
                    transaction_row(source_trans_id="same-a", source_date_created="2026-02-01 10:00:01"),
                ],
            )
            write_transactions(
                root / "outflow.csv",
                [
                    transaction_row(transtype="outflow", source_trans_id="early", source_date_created="2026-02-01 10:00:00"),
                    transaction_row(transtype="outflow", source_trans_id="same-b", source_date_created="2026-02-01 10:00:01"),
                ],
            )
            path = root / "manifest.json"
            path.write_text(
                json.dumps(
                    manifest_data(
                        [
                            stream("in", "inflow.csv"),
                            stream("out", "outflow.csv", direction="outgoing"),
                        ]
                    )
                ),
                encoding="utf-8",
            )
            manifest = load_manifest(path)
            result = build_sorted_chunks(manifest, root / "sort", chunk_size=1)
            events = list(iter_merged_events(result.chunk_paths))
            self.assertEqual([event.fields["source_trans_id"] for event in events], ["early", "same-a", "same-b", "late"])
            self.assertEqual([event.sequence for event in events], [2, 1, 3, 0])

    def test_only_scenarios_supported_by_stream_metadata_are_selected(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            write_minimal_sources(root)
            write_transactions(root / "transactions.csv", [transaction_row()])
            path = root / "manifest.json"
            path.write_text(json.dumps(manifest_data([stream("wallet", "transactions.csv")])), encoding="utf-8")
            names = {item.name for item in build_portable_scenarios(load_manifest(path))}
            self.assertIn("Wallet Transfer Fraud Screening", names)
            self.assertIn("Merchant Abuse Monitoring", names)
            self.assertIn("High Value Transaction Review", names)
            self.assertIn("Staff Transaction Monitoring", names)
            self.assertIn("Regulatory Reporting Review", names)
            self.assertNotIn("Card Payment Authorization Risk", names)
            self.assertNotIn("Bank Transfer Risk Assessment", names)
            self.assertNotIn("Cash-Out Fraud Monitoring", names)

    def test_portable_scenarios_only_reference_transaction_model_fields(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            write_minimal_sources(root)
            write_transactions(root / "transactions.csv", [transaction_row()])
            streams = [
                stream("wallet", "transactions.csv"),
                stream("card", "transactions.csv", channel="card"),
                stream("bank", "transactions.csv", channel="bank"),
                stream("cash", "transactions.csv", channel="cash", direction="outgoing", system_type="cash_out"),
            ]
            path = root / "manifest.json"
            path.write_text(json.dumps(manifest_data(streams)), encoding="utf-8")
            scenarios = build_portable_scenarios(load_manifest(path))
            supported = {
                "transaction_id", "date", "amount", "fees", "currency", "country", "channel", "direction",
                "system_type", "stream_id", "processor", "transaction_type", "payment_type", "channel_id", "source_id",
                "thirdparty_id", "source_account_no", "source_trans_id", "terminal_id", "merchant_id",
                "product_id", "sub_merchant_id", "account_ref", "account_name", "payment_msisdn", "narration",
                "raw_account_ref", "raw_account_name", "raw_timestamp", "source_file",
            }
            referenced: set[str] = set()

            def visit(node: object) -> None:
                if isinstance(node, dict):
                    if node.get("function") == "field_ref":
                        referenced.add(node["named_children"]["field"]["constant"])
                    if node.get("function") == "Filter":
                        referenced.add(node["named_children"]["fieldName"]["constant"])
                    for value in node.values():
                        visit(value)
                elif isinstance(node, list):
                    for value in node:
                        visit(value)

            for scenario in scenarios:
                visit(scenario.trigger_formula)
                for rule in scenario.rules:
                    visit(rule.formula)
            self.assertEqual(referenced - supported, set())
            self.assertIn("Card Payment Authorization Risk", {item.name for item in scenarios})
            self.assertIn("Bank Transfer Risk Assessment", {item.name for item in scenarios})
            self.assertIn("Cash-Out Fraud Monitoring", {item.name for item in scenarios})

    def test_merchant_watchlist_rule_guards_missing_related_merchants(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            write_minimal_sources(root)
            write_transactions(root / "transactions.csv", [transaction_row()])
            manifest = manifest_data([stream("wallet", "transactions.csv")])
            manifest["reference_data"]["merchant_watchlist_xlsx"] = "merchants.xlsx"
            path = root / "manifest.json"
            path.write_text(json.dumps(manifest), encoding="utf-8")

            scenarios = build_portable_scenarios(load_manifest(path))
            merchant_scenario = next(item for item in scenarios if item.name == "Merchant Abuse Monitoring")
            rule = next(item for item in merchant_scenario.rules if item.name == "Watchlisted Merchant Name Match")

            self.assertEqual(rule.formula["function"], "and")
            self.assertEqual(rule.formula["children"][0]["function"], "is_not_empty")
            self.assertEqual(rule.formula["children"][1]["function"], "in_custom_list")


if __name__ == "__main__":
    unittest.main()
