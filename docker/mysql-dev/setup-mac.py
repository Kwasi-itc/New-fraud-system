#!/usr/bin/env python3
"""Python compatibility launcher for the shared cross-platform setup.

The setup implementation is shared with PowerShell in docker/setup.mjs. This
file keeps the existing macOS command working while using the unified gateway
and provider/database migration flow.
"""

import argparse
from pathlib import Path
import shutil
import subprocess
import sys


REPO = Path(__file__).resolve().parents[2]


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--port", type=int, default=8088,
                        help="shared frontend/API/Keycloak gateway port (default: 8088)")
    parser.add_argument("--install", action="store_true",
                        help="run owner migrations and rebuild backend images")
    parser.add_argument("--skip-build", action="store_true",
                        help="reuse the existing frontend image")
    parser.add_argument("--users-file", type=Path,
                        help="optional portable user manifest")
    parser.add_argument("--create-missing-users", action="store_true",
                        help="allow creation of users named by the manifest")
    args = parser.parse_args(argv)

    if not 1024 <= args.port <= 65535:
        parser.error("--port must be between 1024 and 65535")
    if args.create_missing_users and not args.users_file:
        parser.error("--create-missing-users requires --users-file")

    command = ["node", str(REPO / "docker/setup-postgres-ec2.mjs"), "--port", str(args.port)]
    if args.install:
        command.append("--install")
    if args.skip_build:
        command.append("--skip-build")
    if args.users_file:
        manifest = args.users_file.expanduser().resolve()
        if not manifest.is_file():
            parser.error("--users-file must identify an existing file")
        command.extend(["--users-file", str(manifest)])
    if args.create_missing_users:
        command.append("--create-missing-users")

    if not shutil.which("node"):
        parser.error("Node.js 22 or newer is required; install Node.js and retry")
    try:
        result = subprocess.run(command, cwd=REPO, check=False)
    except OSError as exc:
        print(f"Could not start the shared setup: {exc}", file=sys.stderr)
        return 1
    return result.returncode


if __name__ == "__main__":
    raise SystemExit(main())
