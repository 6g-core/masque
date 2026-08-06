import io
import json
import runpy
import tempfile
import unittest
from pathlib import Path
from unittest import mock

from masque_client import MasqueClient, client_from_config, load_runtime_config
from run_client import PAYLOAD, main


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


class FakeClient:
    def __init__(self):
        self.connected = False
        self.closed = False
        self.sent_payloads = []

    def connect(self):
        self.connected = True

    def send(self, payload):
        self.sent_payloads.append(payload)
        return type("Response", (), {"attempts": 1, "payload": "reply: " + payload})()

    def close(self):
        self.closed = True


class MasqueClientTests(unittest.TestCase):
    def test_masque_client_module_is_tool_only(self):
        with mock.patch("run_client.main", side_effect=AssertionError("main should not run")):
            runpy.run_path(str(Path(__file__).parent / "masque_client.py"), run_name="__main__")

    def test_run_client_module_is_executable_entrypoint(self):
        source = (Path(__file__).parent / "run_client.py").read_text(encoding="utf-8")

        self.assertIn('if __name__ == "__main__":', source)
        self.assertIn("raise SystemExit(main())", source)

    def test_payload_contains_flexible_qos_request_json(self):
        payload = json.loads(PAYLOAD)

        self.assertEqual("req-flexible-qos-20260721-001", payload["request_id"])
        self.assertEqual(11222, payload["RNTI"])
        self.assertEqual(1, payload["qfi"])
        self.assertEqual("192.168.1.100", payload["packet_filter"]["src_ip"])
        self.assertEqual(443, payload["packet_filter"]["dst_port"])
        self.assertEqual("VideoStreaming", payload["service_info"]["service_type"])
        self.assertEqual(4.2, payload["service_info"]["service_experience"]["mos_score"])

    def test_load_runtime_config_requires_config_file(self):
        with tempfile.TemporaryDirectory() as tmp:
            config_path = Path(tmp) / "missing.json"

            with self.assertRaisesRegex(FileNotFoundError, "missing.json"):
                load_runtime_config(config_path)

    def test_load_runtime_config_requires_client_fields(self):
        with tempfile.TemporaryDirectory() as tmp:
            config_path = Path(tmp) / "config.json"
            config_path.write_text(json.dumps({"client_exe": "client.exe"}), encoding="utf-8")

            with self.assertRaisesRegex(ValueError, "missing config field"):
                load_runtime_config(config_path)

    def test_load_runtime_config_reads_client_defaults_from_json(self):
        with tempfile.TemporaryDirectory() as tmp:
            config_path = Path(tmp) / "config.json"
            config_path.write_text(
                json.dumps(
                    {
                        "client_exe": "custom-client.exe",
                        "proxy": "https://192.168.1.20:443",
                        "target": "192.168.1.50:7400",
                        "timeout_seconds": 7,
                        "insecure": True,
                        "ttl": "2m",
                        "max_entries": 10000,
                        "max_payload": 1000,
                        "max_retries": 3,
                        "timeout": "1s",
                    }
                ),
                encoding="utf-8",
            )

            cfg = load_runtime_config(config_path)

        self.assertEqual(str(config_path.parent / "custom-client.exe"), cfg.client_exe)
        self.assertEqual(str(config_path), cfg.config)
        self.assertEqual("https://192.168.1.20:443", cfg.proxy)
        self.assertEqual("192.168.1.50:7400", cfg.target)
        self.assertEqual(7, cfg.timeout)
        self.assertTrue(cfg.insecure)

    def test_client_from_config_uses_config_file_without_payload(self):
        with tempfile.TemporaryDirectory() as tmp:
            config_path = Path(tmp) / "config.json"
            config_path.write_text(
                json.dumps(
                    {
                        "client_exe": "custom-client.exe",
                        "proxy": "https://192.168.1.20:443",
                        "target": "192.168.1.50:7400",
                        "timeout_seconds": 7,
                        "insecure": False,
                    }
                ),
                encoding="utf-8",
            )

            client = client_from_config(config_path)

        self.assertEqual(str(config_path.parent / "custom-client.exe"), client.client_exe)
        self.assertEqual(str(config_path), client.config)
        self.assertEqual("https://192.168.1.20:443", client.proxy)
        self.assertEqual("192.168.1.50:7400", client.target)
        self.assertEqual(7, client.timeout)
        self.assertFalse(client.insecure)

    def test_main_sends_hardcoded_payload_from_entrypoint(self):
        fake_client = FakeClient()
        stdout = io.StringIO()

        with (
            mock.patch("run_client.client_from_config", return_value=fake_client),
            mock.patch("run_client.sys.stdout", stdout),
        ):
            code = main([])

        self.assertEqual(0, code)
        self.assertEqual([PAYLOAD], fake_client.sent_payloads)
        self.assertTrue(fake_client.connected)
        self.assertTrue(fake_client.closed)

    def test_main_rejects_command_line_config_overrides(self):
        stderr = io.StringIO()

        with mock.patch("run_client.sys.stderr", stderr):
            code = main(["--target", "192.168.1.60:7400"])

        self.assertEqual(1, code)
        self.assertIn("edit config.json", stderr.getvalue())

    def test_connect_send_close_use_session_protocol(self):
        process = FakeProcess()
        with mock.patch("masque_client.subprocess.Popen", return_value=process) as popen:
            client = MasqueClient(
                client_exe="client.exe",
                config="config.json",
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
