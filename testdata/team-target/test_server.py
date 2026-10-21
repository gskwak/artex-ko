"""Contract tests for the lab target's incremental JSONL feed."""

import importlib.util
import json
from http.server import ThreadingHTTPServer
from pathlib import Path
from tempfile import TemporaryDirectory
from threading import Thread
import unittest
from urllib.error import HTTPError
from urllib.request import urlopen


SPEC = importlib.util.spec_from_file_location("lab_target_server", Path(__file__).with_name("server.py"))
server = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(server)


class FeedTests(unittest.TestCase):
    def setUp(self):
        self.tempdir = TemporaryDirectory()
        self.original_log_path = server.LOG_PATH
        server.LOG_PATH = Path(self.tempdir.name) / "access.jsonl"
        self.http = ThreadingHTTPServer(("127.0.0.1", 0), server.Handler)
        self.thread = Thread(target=self.http.serve_forever, daemon=True)
        self.thread.start()
        self.base = f"http://127.0.0.1:{self.http.server_port}"

    def tearDown(self):
        self.http.shutdown()
        self.http.server_close()
        self.thread.join(timeout=5)
        server.LOG_PATH = self.original_log_path
        self.tempdir.cleanup()

    def fetch(self, path):
        with urlopen(self.base + path, timeout=5) as response:
            return response.read(), response.headers

    def test_new_records_are_incremental_and_feed_does_not_log_itself(self):
        self.fetch("/api/items")
        first, headers = self.fetch("/logs/events?cursor=0")
        self.assertEqual(len(first.splitlines()), 1)
        self.assertEqual(json.loads(first)["url"], "/api/items")
        cursor = headers["X-ARTEX-Next-Cursor"]
        self.assertEqual(int(cursor), len(first))

        empty, headers = self.fetch(f"/logs/events?cursor={cursor}")
        self.assertEqual(empty, b"")
        self.assertEqual(headers["X-ARTEX-Next-Cursor"], cursor)

        self.fetch("/debug/config")
        second, headers = self.fetch(f"/logs/events?cursor={cursor}")
        self.assertEqual(len(second.splitlines()), 1)
        self.assertEqual(json.loads(second)["url"], "/debug/config")
        self.assertGreater(int(headers["X-ARTEX-Next-Cursor"]), int(cursor))
        self.assertEqual(len(server.LOG_PATH.read_bytes().splitlines()), 2)

    def test_bad_cursor_and_page_limit(self):
        with self.assertRaises(HTTPError) as invalid:
            self.fetch("/logs/events?cursor=not-a-number")
        self.assertEqual(invalid.exception.code, 400)
        invalid.exception.close()

        records = [json.dumps({"external_id": str(i)}).encode() + b"\n" for i in range(501)]
        server.LOG_PATH.write_bytes(b"".join(records))
        with self.assertRaises(HTTPError) as middle:
            self.fetch("/logs/events?cursor=1")
        self.assertEqual(middle.exception.code, 400)
        middle.exception.close()

        first, headers = self.fetch("/logs/events?cursor=0")
        self.assertEqual(len(first.splitlines()), 500)
        last, headers = self.fetch(f"/logs/events?cursor={headers['X-ARTEX-Next-Cursor']}")
        self.assertEqual(len(last.splitlines()), 1)
        self.assertEqual(json.loads(last)["external_id"], "500")

    def test_truncated_log_restarts_from_zero(self):
        server.LOG_PATH.write_bytes(b'{"external_id":"long-old-event"}\n')
        _, headers = self.fetch("/logs/events")
        old_cursor = headers["X-ARTEX-Next-Cursor"]
        server.LOG_PATH.write_bytes(b'{"external_id":"new"}\n')
        replay, headers = self.fetch(f"/logs/events?cursor={old_cursor}")
        self.assertEqual(json.loads(replay)["external_id"], "new")
        self.assertEqual(int(headers["X-ARTEX-Next-Cursor"]), len(replay))


if __name__ == "__main__":
    unittest.main()
