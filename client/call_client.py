"""Python wrapper for the MASQUE Go client session mode."""

from __future__ import annotations

import argparse
import json
import subprocess
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Sequence


SCRIPT_DIR = Path(__file__).resolve().parent
DEFAULT_CLIENT_EXE = SCRIPT_DIR / "client.exe"
DEFAULT_CONFIG = SCRIPT_DIR / "reliability.example.json"
DEFAULT_PROXY = "https://192.168.1.45:443"
DEFAULT_TARGET = "192.168.1.45:7400"
DEFAULT_PAYLOAD = "hello"
DEFAULT_TIMEOUT_SECONDS = 10


@dataclass
class SendResponse:
    payload: str
    attempts: int


class MasqueClient:
    def __init__(
        self,
        client_exe: str | Path = DEFAULT_CLIENT_EXE,
        config: str | Path = DEFAULT_CONFIG,
        proxy: str = DEFAULT_PROXY,
        target: str = DEFAULT_TARGET,
        timeout: float = DEFAULT_TIMEOUT_SECONDS,
        insecure: bool = False,
    ) -> None:
        self.client_exe = str(client_exe)
        self.config = str(config)
        self.proxy = proxy
        self.target = target
        self.timeout = timeout
        self.insecure = insecure
        self.proc: subprocess.Popen[str] | None = None

    def connect(self) -> None:
        if self.proc is not None:
            raise RuntimeError("client is already connected")
        self.proc = subprocess.Popen(
            self._command(),
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
        )
        rsp = self._read_response()
        if rsp.get("event") != "ready":
            raise RuntimeError(f"unexpected session response: {rsp}")

    def send(self, payload: str) -> SendResponse:
        self._write_command({"op": "send", "payload": payload})
        rsp = self._read_response()
        return SendResponse(
            payload=str(rsp.get("payload", "")),
            attempts=int(rsp.get("attempts", 0)),
        )

    def close(self) -> None:
        if self.proc is None:
            return
        self._write_command({"op": "close"})
        rsp = self._read_response()
        if rsp.get("event") != "closed":
            raise RuntimeError(f"unexpected close response: {rsp}")
        self.proc.wait(timeout=self.timeout)
        self.proc = None

    def _command(self) -> list[str]:
        cmd = [
            self.client_exe,
            "-session",
            "-config",
            self.config,
            "-proxy",
            self.proxy,
            "-target",
            self.target,
        ]
        if self.insecure:
            cmd.append("-insecure")
        return cmd

    def _write_command(self, command: dict[str, str]) -> None:
        if self.proc is None or self.proc.stdin is None:
            raise RuntimeError("client is not connected")
        self.proc.stdin.write(json.dumps(command) + "\n")
        self.proc.stdin.flush()

    def _read_response(self) -> dict[str, object]:
        if self.proc is None or self.proc.stdout is None:
            raise RuntimeError("client is not connected")
        line = self.proc.stdout.readline()
        if not line:
            stderr = ""
            if self.proc.stderr is not None:
                stderr = self.proc.stderr.read()
            raise RuntimeError(f"client session closed unexpectedly: {stderr}")
        rsp = json.loads(line)
        if not rsp.get("ok"):
            raise RuntimeError(str(rsp.get("error", "client command failed")))
        return rsp


def parse_args(argv: Sequence[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Call client.exe session mode with a Python-style interface.",
    )
    parser.add_argument("--client-exe", default=str(DEFAULT_CLIENT_EXE))
    parser.add_argument("--config", default=str(DEFAULT_CONFIG))
    parser.add_argument("--proxy", default=DEFAULT_PROXY)
    parser.add_argument("--target", default=DEFAULT_TARGET)
    parser.add_argument("--payload", default=DEFAULT_PAYLOAD)
    parser.add_argument("--timeout", type=float, default=DEFAULT_TIMEOUT_SECONDS)
    parser.add_argument("--insecure", action="store_true")
    return parser.parse_args(argv)


def main(argv: Sequence[str] | None = None) -> int:
    args = parse_args(argv)
    client = MasqueClient(
        client_exe=args.client_exe,
        config=args.config,
        proxy=args.proxy,
        target=args.target,
        timeout=args.timeout,
        insecure=args.insecure,
    )
    try:
        client.connect()
        response = client.send(args.payload)
        print(f"received response after {response.attempts} attempt(s): {response.payload}")
        client.close()
    except Exception as exc:
        print(f"client failed: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
