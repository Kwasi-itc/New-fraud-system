"""Run from any directory: python benchmark.py --help."""
from pathlib import Path
import sys

sys.path.insert(0, str(Path(__file__).resolve().parent.parent))
from decision_benchmarks.cli import main

if __name__ == "__main__":
    raise SystemExit(main())
