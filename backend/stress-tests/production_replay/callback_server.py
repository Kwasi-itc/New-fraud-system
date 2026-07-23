from __future__ import annotations

import argparse
import json
import threading
from datetime import datetime, timezone
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from typing import Any


class CallbackRecorder:
    def __init__(self, output_path: Path) -> None:
        self.output_path = output_path
        self.lock = threading.Lock()
        self.count = 0

    def append(self, record: dict[str, Any]) -> None:
        line = json.dumps(record, sort_keys=True) + "\n"
        with self.lock:
            self.output_path.parent.mkdir(parents=True, exist_ok=True)
            with self.output_path.open("a", encoding="utf-8") as handle:
                handle.write(line)
            self.count += 1


def now_iso() -> str:
    return datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")


def build_handler(recorder: CallbackRecorder) -> type[BaseHTTPRequestHandler]:
    class Handler(BaseHTTPRequestHandler):
        def do_GET(self) -> None:
            if self.path == "/readyz":
                self.send_response(200)
                self.end_headers()
                self.wfile.write(b"ok")
                return
            if self.path == "/summary":
                body = json.dumps({"callbacks_received": recorder.count}).encode("utf-8")
                self.send_response(200)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(body)))
                self.end_headers()
                self.wfile.write(body)
                return
            self.send_response(404)
            self.end_headers()

        def do_POST(self) -> None:
            length = int(self.headers.get("Content-Length", "0") or "0")
            raw_body = self.rfile.read(length)
            try:
                body: Any = json.loads(raw_body.decode("utf-8")) if raw_body else None
            except json.JSONDecodeError:
                body = {"raw": raw_body.decode("utf-8", errors="replace")}
            recorder.append(
                {
                    "received_at": now_iso(),
                    "path": self.path,
                    "execution_id_header": self.headers.get("X-Async-Execution-ID", ""),
                    "timestamp_header": self.headers.get("X-Async-Execution-Timestamp", ""),
                    "signature_header": self.headers.get("X-Async-Execution-Signature", ""),
                    "body": body,
                }
            )
            self.send_response(204)
            self.end_headers()

        def log_message(self, _format: str, *_args: Any) -> None:
            return

    return Handler


def main() -> None:
    parser = argparse.ArgumentParser(description="Record async decision callback payloads as NDJSON")
    parser.add_argument("--host", default="0.0.0.0")
    parser.add_argument("--port", type=int, default=8099)
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()

    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text("", encoding="utf-8")
    recorder = CallbackRecorder(args.output)
    server = ThreadingHTTPServer((args.host, args.port), build_handler(recorder))
    print(f"callback server listening on http://{args.host}:{args.port}", flush=True)
    server.serve_forever()


if __name__ == "__main__":
    main()
