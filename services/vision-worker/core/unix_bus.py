"""Minimal JSON-lines publisher for the existing Synora Unix event bus."""

from __future__ import annotations

import json
import socket
import time
import uuid
from typing import Any


class UnixBusPublisher:
    def __init__(self, socket_path: str = "/run/synora/bus.sock", service: str = "vision-worker"):
        self.socket_path = socket_path
        self.service = service
        self._socket: socket.socket | None = None

    def connect(self) -> None:
        if self._socket is not None:
            return
        sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        sock.settimeout(2.0)
        sock.connect(self.socket_path)
        self._write({
            "id": str(uuid.uuid4()), "type": "bus.register", "kind": "command",
            "source": self.service, "source_type": "service",
        }, sock)
        self._socket = sock

    def publish(self, event_type: str, payload: dict[str, Any]) -> None:
        self.connect()
        self._write({
            "id": str(uuid.uuid4()), "type": event_type, "kind": "event",
            "source": self.service, "source_type": "service", "target": "core",
            "timestamp": time.time(), "payload": payload,
        }, self._socket)

    def close(self) -> None:
        if self._socket is not None:
            try:
                self._socket.close()
            finally:
                self._socket = None

    @staticmethod
    def _write(value: dict[str, Any], sock: socket.socket | None) -> None:
        if sock is None:
            raise RuntimeError("bus socket is not connected")
        sock.sendall((json.dumps(value, separators=(",", ":")) + "\n").encode("utf-8"))


