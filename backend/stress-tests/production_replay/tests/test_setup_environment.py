from __future__ import annotations

import unittest

from production_replay.setup_environment import _duration_ms, _parse_iso_time


class SetupEnvironmentTimestampTests(unittest.TestCase):
    def test_parses_go_timestamp_with_five_fractional_digits_on_python_310(self) -> None:
        parsed = _parse_iso_time("2026-09-08T12:52:07.40544+00:00")

        self.assertIsNotNone(parsed)
        self.assertEqual(parsed.microsecond, 405440)  # type: ignore[union-attr]

    def test_parses_nanoseconds_at_python_microsecond_precision(self) -> None:
        parsed = _parse_iso_time("2026-09-08T12:52:07.405441987Z")

        self.assertIsNotNone(parsed)
        self.assertEqual(parsed.microsecond, 405441)  # type: ignore[union-attr]

    def test_calculates_duration_across_variable_precision_timestamps(self) -> None:
        duration = _duration_ms(
            "2026-09-08T12:52:07.40544+00:00",
            "2026-09-08T12:52:08.405441987Z",
        )

        self.assertEqual(duration, 1000.001)
