from __future__ import annotations

import json
import tempfile
import unittest
from pathlib import Path

from production_replay.adapters import get_adapter
from production_replay.manifest import ManifestError, load_manifest
from production_replay.tests.helpers import manifest_data, stream, transaction_row, write_minimal_sources, write_transactions


class ManifestAndAdapterTests(unittest.TestCase):
    def test_relative_paths_and_multiline_csv_are_parsed_logically(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            write_minimal_sources(root)
            embedded = '{\n  "name": "Chango",\n  "idNumber": "V123"\n}'
            write_transactions(root / "transactions.csv", [transaction_row(accountref=embedded, accountname=embedded)])
            path = root / "manifest.json"
            path.write_text(json.dumps(manifest_data([stream("wallet-in", "transactions.csv")])), encoding="utf-8")

            manifest = load_manifest(path)
            configured_stream = manifest.transaction_streams[0]
            events = list(get_adapter(configured_stream.adapter).iter_events(configured_stream, manifest.stream_files(configured_stream)))

            self.assertEqual(len(events), 1)
            self.assertTrue(events[0].object_id.startswith("production-replay:wallet-in:"))
            self.assertEqual(events[0].fields["object_id"], events[0].object_id)
            self.assertEqual(events[0].fields["account_name"], "Chango")
            self.assertIn("\n", events[0].fields["raw_account_ref"])
            self.assertEqual(events[0].fields["country"], "GH")
            self.assertEqual(events[0].fields["amount"], 10.5)
            self.assertEqual(events[0].fields["channel_id"], "MOMO")

    def test_source_rows_version_repeated_transaction_identifiers(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            write_minimal_sources(root)
            write_transactions(root / "transactions.csv", [transaction_row(), transaction_row()])
            path = root / "manifest.json"
            path.write_text(json.dumps(manifest_data([stream("wallet-in", "transactions.csv")])), encoding="utf-8")
            manifest = load_manifest(path)
            configured_stream = manifest.transaction_streams[0]
            events = list(get_adapter(configured_stream.adapter).iter_events(configured_stream, manifest.stream_files(configured_stream)))
            self.assertEqual(len({event.object_id for event in events}), 2)
            self.assertEqual({event.fields["source_trans_id"] for event in events}, {"tx-1"})

    def test_duplicate_stream_ids_are_rejected(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            path = root / "manifest.json"
            path.write_text(
                json.dumps(manifest_data([stream("same", "a.csv"), stream("same", "b.csv")])),
                encoding="utf-8",
            )
            with self.assertRaisesRegex(ManifestError, "duplicate transaction stream id"):
                load_manifest(path)


if __name__ == "__main__":
    unittest.main()
