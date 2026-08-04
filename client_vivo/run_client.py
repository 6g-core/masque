"""Executable entrypoint for the Python MASQUE client wrapper."""

from __future__ import annotations

import sys
from typing import Sequence

from masque_client import client_from_config


PAYLOAD = "hello"

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
