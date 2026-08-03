import io
import json
import unittest
from unittest import mock

from call_client import MasqueClient


class FakeProcess:
    def __init__(self):
        self.stdin = io.StringIO()
        self.stdout = io.StringIO(
            json.dumps({"ok": True, "event": "ready"}) + "\n"
            + json.dumps({"ok": True, "payload": "reply: hello", "attempts": 1}) + "\n"
            + json.dumps({"ok": True, "event": "closed"}) + "\n"
        )
        self.stderr = io.StringIO()
        self.returncode = None
        self.wait_called = False
        self.wait_timeout = None

    def wait(self, timeout=None):
        self.wait_called = True
        self.wait_timeout = timeout
        self.returncode = 0
        return self.returncode


class MasqueClientTests(unittest.TestCase):
    def test_connect_send_close_use_session_protocol(self):
        process = FakeProcess()
        with mock.patch("call_client.subprocess.Popen", return_value=process) as popen:
            client = MasqueClient(
                client_exe="client.exe",
                config="reliability.example.json",
                proxy="https://192.168.1.45:443",
                target="192.168.1.45:7400",
                timeout=3,
                insecure=True,
            )

            client.connect()
            response = client.send("hello")
            client.close()

        command = popen.call_args.args[0]
        self.assertIn("-session", command)
        self.assertIn("-proxy", command)
        self.assertEqual("reply: hello", response.payload)
        self.assertEqual(1, response.attempts)
        self.assertIn(json.dumps({"op": "send", "payload": "hello"}) + "\n", process.stdin.getvalue())
        self.assertIn(json.dumps({"op": "close"}) + "\n", process.stdin.getvalue())
        self.assertTrue(process.wait_called)
        self.assertEqual(3, process.wait_timeout)


if __name__ == "__main__":
    unittest.main()
