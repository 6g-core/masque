"""Executable entrypoint for the Python MASQUE client wrapper."""

from __future__ import annotations

import json
import sys
from http.server import BaseHTTPRequestHandler, HTTPServer
from typing import Sequence

from masque_client import client_from_config


LISTEN_HOST = "0.0.0.0"
LISTEN_PORT = 9666

PAYLOAD = json.dumps(
    {
        "request_id": "req-flexible-qos-20260721-001",
        "RNTI": 1,
        "qfi": 1,
        "packet_filter": {
            "src_ip": "192.168.1.100",
            "dst_ip": "192.168.1.10",
            "src_port": 50000,
            "dst_port": 8787,
            "protocol": 6,
        },
        "traffic_pattern": "streaming",
        "burst_info": {
            "ul_burst_size": 2047,
            "ul_burst_duration": 3000
        },
        "service_info": {
            "e2e_delay": 160
        },
    },
    ensure_ascii=False,
)


def build_qos_handler(client):
    class QosHandler(BaseHTTPRequestHandler):
        def do_POST(self) -> None:
            print(
                f"received HTTP request method=POST path={self.path} "
                f"remote={self.client_address[0]}:{self.client_address[1]}"
            )
            if self.path != "/send_qos":
                self._write_json(404, {"ok": False, "error": "not found"})
                return

            content_length = int(self.headers.get("Content-Length", "0"))
            if content_length:
                self.rfile.read(content_length)

            try:
                response = client.send(PAYLOAD)
            except Exception as exc:
                self._write_json(500, {"ok": False, "error": str(exc)})
                return

            self._write_json(
                200,
                {
                    "ok": True,
                    "attempts": response.attempts,
                    "response": response.payload,
                },
            )

        def log_message(self, format: str, *args) -> None:
            return

        def _write_json(self, status: int, body: dict[str, object]) -> None:
            data = json.dumps(body, ensure_ascii=False).encode("utf-8")
            print(f"HTTP response status={status} body={data.decode('utf-8')}")
            self.send_response(status)
            self.send_header("Content-Type", "application/json; charset=utf-8")
            self.send_header("Content-Length", str(len(data)))
            self.end_headers()
            self.wfile.write(data)

    return QosHandler


def run_qos_server(client, host: str = LISTEN_HOST, port: int = LISTEN_PORT) -> None:
    server = HTTPServer((host, port), build_qos_handler(client))
    print(f"QoS HTTP server listening on http://{host}:{port}/send_qos")
    try:
        server.serve_forever()
    finally:
        server.server_close()


def main(argv: Sequence[str] | None = None) -> int:
    client = None
    try:
        args = list(argv if argv is not None else sys.argv[1:])
        if args:
            raise ValueError("command line overrides are not supported; edit config.json")
        client = client_from_config()
        run_qos_server(client)
    except KeyboardInterrupt:
        print("client stopped")
    except Exception as exc:
        print(f"client failed: {exc}", file=sys.stderr)
        return 1
    finally:
        if client is not None:
            client.close()
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
