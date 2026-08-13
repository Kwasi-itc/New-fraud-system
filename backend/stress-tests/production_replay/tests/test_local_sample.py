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
from production_replay.adapters import get_adapter
from production_replay.manifest import load_manifest
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

    def test_total_sample_uses_other_streams_when_one_stream_is_short(self) -> None:
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
                row_count = 1 if stream_id == "genpay-inflow" else 4
                write_transactions(
                    source_path,
                    [
                        transaction_row(source_trans_id=f"{stream_id}-{index}")
                        for index in range(1, row_count + 1)
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
            stream_counts = {
                configured_stream["id"]: _csv_data_rows(Path(configured_stream["globs"][0]))
                for configured_stream in generated_manifest["transaction_streams"]
            }
            self.assertEqual(stream_counts["genpay-inflow"], 1)
            self.assertEqual(sum(stream_counts.values()), 12)

    def test_total_sample_reports_combined_capacity_when_sources_are_short(self) -> None:
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
                write_transactions(source_path, [transaction_row(source_trans_id=f"{stream_id}-1")])

            manifest_path.write_text(
                json.dumps(manifest_data([stream(stream_id, "unused.csv") for stream_id in STREAM_SOURCE_PATHS])),
                encoding="utf-8",
            )

            with self.assertRaisesRegex(ValueError, "configured transaction streams contain 6 rows; 7 are required"):
                create_local_sample(
                    manifest_path,
                    data_root,
                    output_dir,
                    output_manifest,
                    total_transactions=7,
                )

    def test_total_sample_can_skip_globally_ordered_transactions(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            data_root = root / "data-root"
            first_output_dir = root / "first-sample"
            next_output_dir = root / "next-sample"
            manifest_path = root / "manifest.json"
            first_manifest = root / "first-manifest.json"
            next_manifest = root / "next-manifest.json"

            data_root.mkdir(parents=True)
            write_minimal_sources(data_root)
            for stream_id, relative_path in STREAM_SOURCE_PATHS.items():
                source_path = data_root / relative_path
                source_path.parent.mkdir(parents=True, exist_ok=True)
                write_transactions(
                    source_path,
                    [
                        transaction_row(
                            source_trans_id=f"{stream_id}-{index}",
                            source_date_created=f"2026-06-01 00:00:0{index}",
                        )
                        for index in range(1, 4)
                    ],
                )

            manifest_path.write_text(
                json.dumps(manifest_data([stream(stream_id, "unused.csv") for stream_id in STREAM_SOURCE_PATHS])),
                encoding="utf-8",
            )

            create_local_sample(
                manifest_path,
                data_root,
                first_output_dir,
                first_manifest,
                total_transactions=6,
            )
            create_local_sample(
                manifest_path,
                data_root,
                next_output_dir,
                next_manifest,
                total_transactions=6,
                transaction_offset=6,
            )

            first_ids = _sample_source_ids(first_manifest)
            next_ids = _sample_source_ids(next_manifest)
            self.assertEqual(len(first_ids), 6)
            self.assertEqual(len(next_ids), 6)
            self.assertTrue(first_ids.isdisjoint(next_ids))

    def test_total_sample_offset_is_included_in_capacity_check(self) -> None:
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
                write_transactions(source_path, [transaction_row(source_trans_id=f"{stream_id}-1")])

            manifest_path.write_text(
                json.dumps(manifest_data([stream(stream_id, "unused.csv") for stream_id in STREAM_SOURCE_PATHS])),
                encoding="utf-8",
            )

            with self.assertRaisesRegex(ValueError, "configured transaction streams contain 6 rows; 7 are required"):
                create_local_sample(
                    manifest_path,
                    data_root,
                    output_dir,
                    output_manifest,
                    total_transactions=2,
                    transaction_offset=5,
                )

    def test_offset_preserves_object_ids_for_an_accidentally_overlapping_range(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            data_root = root / "data-root"
            output_dir = root / "sample"
            manifest_path = root / "manifest.json"
            first_manifest = root / "first-manifest.json"
            overlap_manifest = root / "overlap-manifest.json"

            data_root.mkdir(parents=True)
            write_minimal_sources(data_root)
            for stream_id, relative_path in STREAM_SOURCE_PATHS.items():
                source_path = data_root / relative_path
                source_path.parent.mkdir(parents=True, exist_ok=True)
                write_transactions(
                    source_path,
                    [
                        transaction_row(
                            source_trans_id="" if index == 2 else f"{stream_id}-{index}",
                            thirdparty_id="" if index == 2 else f"third-{stream_id}-{index}",
                            source_date_created=f"2026-06-01 00:00:0{index}",
                        )
                        for index in range(1, 4)
                    ],
                )

            manifest_path.write_text(
                json.dumps(manifest_data([stream(stream_id, "unused.csv") for stream_id in STREAM_SOURCE_PATHS])),
                encoding="utf-8",
            )

            create_local_sample(
                manifest_path,
                data_root,
                output_dir,
                first_manifest,
                total_transactions=12,
            )
            first_object_ids = _sample_object_ids(first_manifest)

            create_local_sample(
                manifest_path,
                data_root,
                output_dir,
                overlap_manifest,
                total_transactions=6,
                transaction_offset=6,
            )
            overlap_object_ids = _sample_object_ids(overlap_manifest)

            self.assertEqual(len(overlap_object_ids), 6)
            self.assertTrue(overlap_object_ids.issubset(first_object_ids))

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

    def test_full_manifest_can_use_separate_transaction_and_reference_roots(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            seed_root = root / "fraud_data_seed"
            reference_root = root / "fraud_data"
            manifest_path = root / "manifest.json"
            output_manifest = root / "seed-manifest.json"
            manifest_path.write_text(
                json.dumps(manifest_data([stream(stream_id, "unused.csv") for stream_id in STREAM_SOURCE_PATHS])),
                encoding="utf-8",
            )

            create_full_manifest(
                manifest_path,
                seed_root,
                output_manifest,
                reference_data_root=reference_root,
            )

            generated = json.loads(output_manifest.read_text(encoding="utf-8"))
            self.assertTrue(generated["reference_data"]["staff_csv"].startswith(str(reference_root)))
            for configured_stream in generated["transaction_streams"]:
                self.assertTrue(configured_stream["globs"][0].startswith(str(seed_root)))


def _csv_data_rows(path: Path) -> int:
    with path.open("r", encoding="utf-8", newline="") as handle:
        return sum(1 for _ in csv.DictReader(handle))


def _sample_source_ids(manifest_path: Path) -> set[str]:
    manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    result: set[str] = set()
    for configured_stream in manifest["transaction_streams"]:
        with Path(configured_stream["globs"][0]).open("r", encoding="utf-8", newline="") as handle:
            result.update(row["source_trans_id"] for row in csv.DictReader(handle))
    return result


def _sample_object_ids(manifest_path: Path) -> set[str]:
    manifest = load_manifest(manifest_path)
    return {
        event.object_id
        for configured_stream in manifest.transaction_streams
        for event in get_adapter(configured_stream.adapter).iter_events(
            configured_stream,
            manifest.stream_files(configured_stream),
        )
    }


if __name__ == "__main__":
    unittest.main()
