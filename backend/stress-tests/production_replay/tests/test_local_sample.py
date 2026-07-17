from __future__ import annotations

import csv
import json
import tempfile
import unittest
from pathlib import Path

from production_replay.local_sample import (
    STREAM_SOURCE_PATHS,
    create_duration_sample,
    create_full_manifest,
    create_local_sample,
    parse_duration,
)
from production_replay.tests.helpers import manifest_data, stream, transaction_row, write_minimal_sources, write_transactions


class LocalSampleTests(unittest.TestCase):
    def test_creates_requested_total_across_all_streams(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            data_root = root / "data-root"
            output_dir = root / "sample"
            manifest_path = root / "manifest.json"
            output_manifest = root / "sample-manifest.json"

            data_root.mkdir(parents=True)
            write_minimal_sources(data_root)
            for stream_id, relative_path in STREAM_SOURCE_PATHS.items():
                source_path = data_root / relative_path
                source_path.parent.mkdir(parents=True, exist_ok=True)
                write_transactions(
                    source_path,
                    [
                        transaction_row(source_trans_id=f"{stream_id}-{index}")
                        for index in range(1, 5)
                    ],
                )

            manifest_path.write_text(
                json.dumps(manifest_data([stream(stream_id, "unused.csv") for stream_id in STREAM_SOURCE_PATHS])),
                encoding="utf-8",
            )

            total = create_local_sample(
                manifest_path,
                data_root,
                output_dir,
                output_manifest,
                total_transactions=12,
            )

            self.assertEqual(total, 12)
            generated_manifest = json.loads(output_manifest.read_text(encoding="utf-8"))
            self.assertEqual(len(generated_manifest["transaction_streams"]), len(STREAM_SOURCE_PATHS))
            generated_total = sum(
                _csv_data_rows(Path(configured_stream["globs"][0]))
                for configured_stream in generated_manifest["transaction_streams"]
            )
            self.assertEqual(generated_total, 12)

    def test_transaction_sample_can_span_multiple_files_per_stream(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            data_root = root / "data-root"
            output_dir = root / "sample"
            manifest_path = root / "manifest.json"
            output_manifest = root / "sample-manifest.json"

            data_root.mkdir(parents=True)
            write_minimal_sources(data_root)
            for stream_id, relative_path in STREAM_SOURCE_PATHS.items():
                first_path = data_root / relative_path
                second_path = first_path.with_name("2026-06-02.csv")
                first_path.parent.mkdir(parents=True, exist_ok=True)
                write_transactions(
                    first_path,
                    [
                        transaction_row(
                            source_trans_id=f"{stream_id}-1",
                            source_date_created="2026-06-01 00:00:00",
                        )
                    ],
                )
                write_transactions(
                    second_path,
                    [
                        transaction_row(
                            source_trans_id=f"{stream_id}-2",
                            source_date_created="2026-06-02 00:00:00",
                        ),
                        transaction_row(
                            source_trans_id=f"{stream_id}-3",
                            source_date_created="2026-06-02 00:01:00",
                        ),
                    ],
                )

            manifest_path.write_text(
                json.dumps(manifest_data([stream(stream_id, "unused.csv") for stream_id in STREAM_SOURCE_PATHS])),
                encoding="utf-8",
            )

            total = create_local_sample(
                manifest_path,
                data_root,
                output_dir,
                output_manifest,
                stream_counts={stream_id: 3 for stream_id in STREAM_SOURCE_PATHS},
            )

            self.assertEqual(total, 18)
            generated_manifest = json.loads(output_manifest.read_text(encoding="utf-8"))
            generated_total = sum(
                _csv_data_rows(Path(configured_stream["globs"][0]))
                for configured_stream in generated_manifest["transaction_streams"]
            )
            self.assertEqual(generated_total, 18)

    def test_creates_duration_sample_across_all_streams(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            data_root = root / "data-root"
            output_dir = root / "sample"
            manifest_path = root / "manifest.json"
            output_manifest = root / "duration-manifest.json"

            data_root.mkdir(parents=True)
            for stream_id, relative_path in STREAM_SOURCE_PATHS.items():
                source_path = data_root / relative_path
                source_path.parent.mkdir(parents=True, exist_ok=True)
                write_transactions(
                    source_path,
                    [
                        transaction_row(
                            source_trans_id=f"{stream_id}-1",
                            source_date_created="2026-06-01 00:00:00",
                        ),
                        transaction_row(
                            source_trans_id=f"{stream_id}-2",
                            source_date_created="2026-06-01 00:30:00",
                        ),
                        transaction_row(
                            source_trans_id=f"{stream_id}-3",
                            source_date_created="2026-06-01 01:30:00",
                        ),
                    ],
                )

            manifest_path.write_text(
                json.dumps(manifest_data([stream(stream_id, "unused.csv") for stream_id in STREAM_SOURCE_PATHS])),
                encoding="utf-8",
            )

            total = create_duration_sample(
                manifest_path,
                data_root,
                output_dir,
                output_manifest,
                parse_duration("1h"),
            )

            self.assertEqual(total, 12)
            generated_manifest = json.loads(output_manifest.read_text(encoding="utf-8"))
            generated_total = sum(
                _csv_data_rows(Path(configured_stream["globs"][0]))
                for configured_stream in generated_manifest["transaction_streams"]
            )
            self.assertEqual(generated_total, 12)

    def test_full_manifest_uses_selected_data_root(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            data_root = root / "data-root"
            manifest_path = root / "manifest.json"
            output_manifest = root / "full-manifest.json"

            manifest_path.write_text(
                json.dumps(manifest_data([stream(stream_id, "unused.csv") for stream_id in STREAM_SOURCE_PATHS])),
                encoding="utf-8",
            )

            create_full_manifest(manifest_path, data_root, output_manifest)

            generated_manifest = json.loads(output_manifest.read_text(encoding="utf-8"))
            self.assertEqual(
                generated_manifest["reference_data"]["staff_csv"],
                str(data_root / "data/lists/fraud-staff.csv"),
            )
            for configured_stream in generated_manifest["transaction_streams"]:
                self.assertTrue(configured_stream["globs"][0].startswith(str(data_root)))
                self.assertTrue(configured_stream["globs"][0].endswith("*.csv"))


def _csv_data_rows(path: Path) -> int:
    with path.open("r", encoding="utf-8", newline="") as handle:
        return sum(1 for _ in csv.DictReader(handle))


if __name__ == "__main__":
    unittest.main()
