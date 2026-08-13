from __future__ import annotations

import json
import tempfile
import unittest
from pathlib import Path

from production_replay.callback_report import read_new_ndjson


class CallbackReportTests(unittest.TestCase):
    def test_read_new_ndjson_only_returns_appended_records(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            path = Path(temp_dir) / "callbacks.ndjson"
            first = {"body": {"execution_id": "execution-1"}}
            second = {"body": {"execution_id": "execution-2"}}
            path.write_text(json.dumps(first) + "\n", encoding="utf-8")

            records, offset = read_new_ndjson(path, 0)
            self.assertEqual(records, [first])

            with path.open("a", encoding="utf-8") as handle:
                handle.write(json.dumps(second) + "\n")

            records, next_offset = read_new_ndjson(path, offset)
            self.assertEqual(records, [second])
            self.assertGreater(next_offset, offset)


if __name__ == "__main__":
    unittest.main()
