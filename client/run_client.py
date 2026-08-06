"""Executable entrypoint for the Python MASQUE client wrapper."""

from __future__ import annotations

import json
import sys
from typing import Sequence

from masque_client import client_from_config


PAYLOAD = json.dumps(
    {
        "request_id": "req-flexible-qos-20260721-001",
        "RNTI": 11222,
        "qfi": 1,
        "packet_filter": {
            "src_ip": "192.168.1.100",
            "dst_ip": "10.0.0.5",
            "src_port": 50000,
            "dst_port": 443,
            "protocol": 6,
        },
        "traffic_pattern": "streaming",
        "source_address": "192.168.1.100",
        "burst_info": {
            "ul_burst_size": 2048,
            "ul_burst_duration": 100,
            "arrive_time_to_next_burst": 50,
        },
        "service_info": {
            "service_type": "VideoStreaming",
            "frame_rate": 30,
            "max_frame_size": 150000,
            "resolution": "1920x1080",
            "code_rate": 5000,
            "e2e_delay": 160,
            "service_experience": {
                "mos_score": 4.2,
            },
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
