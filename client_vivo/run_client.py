"""Executable entrypoint for the Python MASQUE client wrapper."""

from __future__ import annotations

import json
import sys
from typing import Sequence

from masque_client import client_from_config


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


def main(argv: Sequence[str] | None = None) -> int:
    try:
        args = list(argv if argv is not None else sys.argv[1:])
        if args:
            raise ValueError("command line overrides are not supported; edit config.json")
        client = client_from_config()
        client.connect()
        response = client.send(PAYLOAD)
        print(f"received response after {response.attempts} attempt(s): {response.payload}")
        client.close()
    except Exception as exc:
        print(f"client failed: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
