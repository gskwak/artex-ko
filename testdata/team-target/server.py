"""Private, fake-data HTTP target for ARTEX Red/Blue workflow smoke tests."""

from datetime import datetime, timezone
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
from pathlib import Path
from threading import Lock
from urllib.parse import urlsplit
from uuid import uuid4


PORT = 8080
LOG_PATH = Path("/logs/access.jsonl")
LOG_LOCK = Lock()


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):  # noqa: N802 - http.server naming convention
        self._respond(include_body=True)

    def do_HEAD(self):  # noqa: N802 - http.server naming convention
        self._respond(include_body=False)

    def do_POST(self):  # noqa: N802 - http.server naming convention
        self._respond(include_body=True)

    def _respond(self, *, include_body: bool):
        route = urlsplit(self.path).path
        if self.command not in ("GET", "HEAD"):
            status, mime, payload = 405, "application/json", {"error": "method not allowed"}
        elif route == "/health":
            status, mime, payload = 200, "application/json", {"status": "ok"}
        elif route == "/":
            status, mime, payload = 200, "text/html; charset=utf-8", (
                "<!doctype html><html><title>ARTEX test target</title>"
                "<body><h1>ARTEX test target</h1>"
                "<p>Private lab fixture with fake data only.</p>"
                "<ul><li><a href='/api/items'>Items</a></li>"
                "<li><a href='/debug/config'>Debug config</a></li>"
                "<li><a href='/admin'>Admin</a></li></ul></body></html>"
            )
        elif route == "/api/items":
            status, mime, payload = 200, "application/json", {
                "items": [
                    {"id": 1, "name": "sample-item", "owner": "lab-user"},
                    {"id": 2, "name": "sample-report", "owner": "lab-user"},
                ]
            }
        elif route == "/debug/config":
            # Intentional, harmless disclosure for a Red finding and Blue investigation.
            status, mime, payload = 200, "application/json", {
                "training_only": True,
                "issue": "unauthenticated_debug_configuration",
                "credential": "DEMO_ONLY_NOT_A_REAL_SECRET",
            }
        elif route == "/admin":
            status, mime, payload = 403, "application/json", {"error": "forbidden"}
        else:
            status, mime, payload = 404, "application/json", {"error": "not found"}

        body = payload.encode("utf-8") if isinstance(payload, str) else json.dumps(payload).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", mime)
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Cache-Control", "no-store")
        self.end_headers()
        if include_body:
            self.wfile.write(body)
        # Health probes run every five seconds; they are not investigation data.
        if route != "/health":
            self._record(route, status)

    def _record(self, route: str, status: int):
        disclosure = route == "/debug/config" and status == 200
        event = {
            "source": "artex-lab-target",
            "external_id": uuid4().hex,
            "event_type": "http.request",
            "severity": "medium" if disclosure else "info",
            "asset": "artex-lab-target:8080",
            "message": f"{self.command} {route} returned {status}"
            + ("; unauthenticated demo config disclosure" if disclosure else ""),
            "occurred_at": datetime.now(timezone.utc).isoformat().replace("+00:00", "Z"),
            "src_ip": self.client_address[0],
            "src_port": self.client_address[1],
            "dest_ip": self.connection.getsockname()[0],
            "dest_port": PORT,
            "proto": "TCP",
            "app_proto": "HTTP",
            "host": self.headers.get("Host", ""),
            "method": self.command,
            "url": self.path,
            "status": status,
        }
        with LOG_LOCK:
            with LOG_PATH.open("a", encoding="utf-8") as output:
                output.write(json.dumps(event, ensure_ascii=False, separators=(",", ":")) + "\n")

    def log_message(self, format, *args):
        # The structured JSONL file is the access log; keep container stdout quiet.
        pass


if __name__ == "__main__":
    ThreadingHTTPServer(("0.0.0.0", PORT), Handler).serve_forever()
