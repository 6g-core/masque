import io
import json
import unittest
from http.server import HTTPServer
from threading import Thread
from urllib.error import HTTPError
from urllib.request import Request, urlopen
from unittest import mock

from run_client import PAYLOAD, build_qos_handler


class FakeResponse:
    attempts = 1
    payload = "reply: qos"


class FakeClient:
    def __init__(self):
        self.sent_payloads = []

    def send(self, payload):
        self.sent_payloads.append(payload)
        return FakeResponse()


class RunClientServerTests(unittest.TestCase):
    def test_send_qos_posts_hardcoded_payload_and_returns_response(self):
        client = FakeClient()
        stdout = io.StringIO()
        server = HTTPServer(("127.0.0.1", 0), build_qos_handler(client))
        thread = Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            url = f"http://127.0.0.1:{server.server_port}/send_qos"
            req = Request(url, data=b"", method="POST")

            with mock.patch("sys.stdout", stdout), urlopen(req, timeout=3) as rsp:
                body = json.loads(rsp.read().decode("utf-8"))

            self.assertEqual(200, rsp.status)
            self.assertEqual([PAYLOAD], client.sent_payloads)
            self.assertEqual({"ok": True, "attempts": 1, "response": "reply: qos"}, body)
            logs = stdout.getvalue()
            self.assertIn("received HTTP request method=POST path=/send_qos", logs)
            self.assertIn('HTTP response status=200 body={"ok": true', logs)
        finally:
            server.shutdown()
            thread.join(timeout=3)
            server.server_close()

    def test_send_qos_returns_404_for_other_path(self):
        client = FakeClient()
        stdout = io.StringIO()
        server = HTTPServer(("127.0.0.1", 0), build_qos_handler(client))
        thread = Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            url = f"http://127.0.0.1:{server.server_port}/unknown"
            req = Request(url, data=b"", method="POST")

            with mock.patch("sys.stdout", stdout):
                with self.assertRaises(HTTPError) as ctx:
                    urlopen(req, timeout=3)

            self.assertEqual(404, ctx.exception.code)
            self.assertEqual([], client.sent_payloads)
            self.assertIn("received HTTP request method=POST path=/unknown", stdout.getvalue())
            self.assertIn('HTTP response status=404 body={"ok": false', stdout.getvalue())
        finally:
            server.shutdown()
            thread.join(timeout=3)
            server.server_close()


if __name__ == "__main__":
    unittest.main()
