"""Python wrapper for the MASQUE Go client session mode."""

from __future__ import annotations

import json
import subprocess
from dataclasses import dataclass
from pathlib import Path


SCRIPT_DIR = Path(__file__).resolve().parent
DEFAULT_CONFIG = SCRIPT_DIR / "config.json"
CONFIG_FIELDS = (
    "client_exe",
    "proxy",
    "target",
    "timeout_seconds",
    "insecure",
)


@dataclass
class RuntimeConfig:
    client_exe: str
    config: str
    proxy: str
    target: str
    timeout: float
    insecure: bool


@dataclass
class SendResponse:
    payload: str
    attempts: int


class MasqueClient:
    def __init__(
        self,
        client_exe: str | Path,
        config: str | Path,
        proxy: str,
        target: str,
        timeout: float,
        insecure: bool,
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
        for attempt in range(2):
            if self.proc is None:
                try:
                    self.connect()
                except Exception:
                    self._discard_process()
                    if attempt == 1:
                        raise
                    continue
            try:
                return self._send_connected(payload)
            except Exception as exc:
                if not self._is_session_failure(exc):
                    raise
                self._discard_process()
                if attempt == 1:
                    raise
        raise RuntimeError("failed to establish client session")

    def _send_connected(self, payload: str) -> SendResponse:
        self._write_command({"op": "send", "payload": payload})
        rsp = self._read_response()
        return SendResponse(
            payload=str(rsp.get("payload", "")),
            attempts=int(rsp.get("attempts", 0)),
        )

    @staticmethod
    def _is_session_failure(exc: Exception) -> bool:
        if isinstance(exc, (BrokenPipeError, OSError, json.JSONDecodeError)):
            return True
        message = str(exc).lower()
        return (
            "send payload:" in message
            or "client session closed unexpectedly" in message
        )

    def _discard_process(self) -> None:
        proc = self.proc
        self.proc = None
        if proc is None:
            return
        proc.terminate()
        try:
            proc.wait(timeout=self.timeout)
        except subprocess.TimeoutExpired:
            proc.kill()
            proc.wait(timeout=self.timeout)

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


def load_runtime_config(config_path: str | Path) -> RuntimeConfig:
    path = Path(config_path)
    if not path.exists():
        raise FileNotFoundError(f"config file not found: {path}")
    raw = json.loads(path.read_text(encoding="utf-8"))
    missing = [field for field in CONFIG_FIELDS if field not in raw]
    if missing:
        raise ValueError(f"missing config field(s): {', '.join(missing)}")
    client_exe = Path(str(raw["client_exe"]))
    if not client_exe.is_absolute():
        client_exe = path.parent / client_exe
    return RuntimeConfig(
        client_exe=str(client_exe),
        config=str(path),
        proxy=str(raw["proxy"]),
        target=str(raw["target"]),
        timeout=float(raw["timeout_seconds"]),
        insecure=bool(raw["insecure"]),
    )


def client_from_config(config_path: str | Path = DEFAULT_CONFIG) -> MasqueClient:
    cfg = load_runtime_config(config_path)
    return MasqueClient(
        client_exe=cfg.client_exe,
        config=cfg.config,
        proxy=cfg.proxy,
        target=cfg.target,
        timeout=cfg.timeout,
        insecure=cfg.insecure,
    )
