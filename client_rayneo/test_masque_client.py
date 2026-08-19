import io
import json
import unittest
from unittest import mock

from masque_client import MasqueClient


class FakeProcess:
    def __init__(self, responses):
        self.stdin = io.StringIO()
        self.stdout = io.StringIO(
            "".join(json.dumps(response) + "\n" for response in responses)
        )
        self.stderr = io.StringIO()
        self.terminated = False

    def terminate(self):
        self.terminated = True

    def wait(self, timeout=None):
        return 0


def new_client():
    return MasqueClient(
        client_exe="client.exe",
        config="config.json",
        proxy="https://proxy.example:443",
        target="target.example:7400",
        timeout=1,
        insecure=False,
    )


class MasqueClientLazySessionTests(unittest.TestCase):
    def test_first_send_connects_and_later_sends_reuse_session(self):
        process = FakeProcess(
            [
                {"ok": True, "event": "ready"},
                {"ok": True, "payload": "reply: one", "attempts": 1},
                {"ok": True, "payload": "reply: two", "attempts": 1},
            ]
        )
        client = new_client()

        with mock.patch("masque_client.subprocess.Popen", return_value=process) as popen:
            self.assertIsNone(client.proc)
            first = client.send("one")
            second = client.send("two")

        self.assertEqual("reply: one", first.payload)
        self.assertEqual("reply: two", second.payload)
        self.assertEqual(1, popen.call_count)

    def test_send_reconnects_once_after_session_failure(self):
        expired = FakeProcess(
            [
                {"ok": True, "event": "ready"},
                {
                    "ok": False,
                    "error": "send payload: timeout: no recent network activity",
                },
            ]
        )
        replacement = FakeProcess(
            [
                {"ok": True, "event": "ready"},
                {"ok": True, "payload": "reply: qos", "attempts": 1},
            ]
        )
        client = new_client()

        with mock.patch(
            "masque_client.subprocess.Popen", side_effect=[expired, replacement]
        ) as popen:
            response = client.send("qos")

        self.assertEqual("reply: qos", response.payload)
        self.assertEqual(2, popen.call_count)
        self.assertTrue(expired.terminated)

    def test_send_does_not_reconnect_after_target_response_failure(self):
        process = FakeProcess(
            [
                {"ok": True, "event": "ready"},
                {
                    "ok": False,
                    "error": "request req-1 failed after 3 retries: read response timeout",
                },
            ]
        )
        client = new_client()

        with mock.patch("masque_client.subprocess.Popen", return_value=process) as popen:
            with self.assertRaisesRegex(RuntimeError, "failed after 3 retries"):
                client.send("qos")

        self.assertEqual(1, popen.call_count)
        self.assertFalse(process.terminated)


if __name__ == "__main__":
    unittest.main()
