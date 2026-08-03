from __future__ import annotations

import unittest

from production_replay.cli import build_parser


class CLITests(unittest.TestCase):
    def test_run_defaults_to_async_decision_mode(self) -> None:
        parser = build_parser()
        args = parser.parse_args(
            [
                "run",
                "--manifest",
                "manifest.json",
            ]
        )
        self.assertEqual(args.decision_mode, "async")


if __name__ == "__main__":
    unittest.main()
