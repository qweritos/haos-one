"""Docker API rewrite rules for the compat proxy."""

from __future__ import annotations

import json
import re
from typing import Any
from urllib.parse import urlsplit

_TARGET_RE = re.compile(r"^/(?:v[^/]+/)?(info|containers/json)$")
_HIDDEN_CONTAINER_NAMES = {"/haos_one_compat", "haos_one_compat"}


def normalize_target_path(path: str) -> str | None:
    """Normalize Docker API target paths across versioned and raw endpoints."""
    match = _TARGET_RE.match(urlsplit(path).path)
    if match is None:
        return None
    return f"/{match.group(1)}"


def is_rewrite_target(path: str) -> bool:
    """Return True when the request path should be rewritten."""
    return normalize_target_path(path) is not None


def rewrite_json_payload(path: str, payload: bytes) -> bytes:
    """Rewrite the JSON body for supported Docker API endpoints."""
    target = normalize_target_path(path)
    if target is None:
        return payload

    try:
        data = json.loads(payload.decode("utf-8"))
    except (UnicodeDecodeError, json.JSONDecodeError):
        return payload

    if target == "/info":
        if not isinstance(data, dict):
            return payload
        data["HAOSCompat"] = "intercepted"
    elif target == "/containers/json":
        if not isinstance(data, list):
            return payload
        data = [
            container
            for container in data
            if not _should_hide_container(container)
        ]

    return json.dumps(data, separators=(",", ":")).encode("utf-8")


def _should_hide_container(container: Any) -> bool:
    """Return True when a container should be hidden from the Docker API."""
    if not isinstance(container, dict):
        return False

    names = container.get("Names")
    if isinstance(names, list) and any(name in _HIDDEN_CONTAINER_NAMES for name in names):
        return True

    if container.get("Image") == "haos_one_compat":
        return True

    return False
