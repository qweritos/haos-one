"""Docker API rewrite rules for the compat proxy."""

from __future__ import annotations

import json
import re
from typing import Any
from urllib.parse import parse_qs, urlsplit

_RESPONSE_TARGET_RE = re.compile(r"^/(?:v[^/]+/)?(info|containers/json)$")
_CREATE_TARGET_RE = re.compile(r"^/(?:v[^/]+/)?containers/create$")
_HIDDEN_CONTAINER_NAMES = {"/haos_one_compat", "haos_one_compat"}
_INFO_WARNING = "HAOS compat: intercepted"
_UDEV_SHIM_BIND = "/opt/haos-one-compat/udev-shim:/opt/haos-udev-shim:ro"
_UDEV_SHIM_PATH = "/opt/haos-udev-shim"


def normalize_target_path(path: str) -> str | None:
    """Normalize Docker API target paths across versioned and raw endpoints."""
    match = _RESPONSE_TARGET_RE.match(urlsplit(path).path)
    if match is None:
        return None
    return f"/{match.group(1)}"


def is_rewrite_target(path: str) -> bool:
    """Return True when the request path should be rewritten."""
    return normalize_target_path(path) is not None


def is_create_request(path: str) -> bool:
    """Return whether a Docker API path creates a container."""
    return _CREATE_TARGET_RE.match(urlsplit(path).path) is not None


def rewrite_create_request_payload(
    path: str,
    payload: bytes,
    *,
    inject_udev_shim: bool = False,
    setup_port: str | None = None,
) -> bytes:
    """Remove container-create options unsupported by nested unprivileged LXC."""
    if not is_create_request(path):
        return payload

    try:
        data = json.loads(payload.decode("utf-8"))
    except (UnicodeDecodeError, json.JSONDecodeError):
        return payload

    if not isinstance(data, dict):
        return payload

    changed = "Domainname" in data
    data.pop("Domainname", None)

    host_config = data.get("HostConfig")
    if isinstance(host_config, dict) and "Ulimits" in host_config:
        del host_config["Ulimits"]
        changed = True

    if inject_udev_shim and _is_supervisor_create(path, data):
        changed = _inject_udev_shim(data) or changed

    if setup_port and _is_homeassistant_create(path, data):
        changed = _inject_env(data, "SETUP_PORT", setup_port) or changed
        if _is_homeassistant_landingpage_create(data):
            changed = _publish_landingpage_port(data, setup_port) or changed

    if not changed:
        return payload

    return json.dumps(data, separators=(",", ":")).encode("utf-8")


def user_namespace_is_remapped(uid_map: str) -> bool:
    """Return whether uid 0 is mapped away from host uid 0."""
    try:
        inside, outside, _length = (
            int(value) for value in uid_map.splitlines()[0].split()
        )
    except (IndexError, ValueError):
        return False
    return inside == 0 and outside != 0


def udev_shim_enabled(mode: str, uid_map: str) -> bool:
    """Resolve USE_UDEV_SHIM mode using the current user namespace mapping."""
    normalized = mode.strip().lower() or "auto"
    if normalized == "force":
        return True
    if normalized == "off":
        return False
    if normalized == "auto":
        return user_namespace_is_remapped(uid_map)
    raise ValueError("USE_UDEV_SHIM must be one of: auto, force, off")


def _is_supervisor_create(path: str, data: dict[str, Any]) -> bool:
    target = urlsplit(path)
    names = parse_qs(target.query).get("name", [])
    if any(name.lstrip("/") == "hassio_supervisor" for name in names):
        return True

    image = data.get("Image")
    return isinstance(image, str) and "hassio-supervisor" in image.lower()


def _is_homeassistant_create(path: str, data: dict[str, Any]) -> bool:
    target = urlsplit(path)
    names = parse_qs(target.query).get("name", [])
    return any(name.lstrip("/") == "homeassistant" for name in names)


def _is_homeassistant_landingpage_create(data: dict[str, Any]) -> bool:
    image = data.get("Image")
    return isinstance(image, str) and image.endswith(":landingpage")


def _publish_landingpage_port(data: dict[str, Any], setup_port: str) -> bool:
    try:
        port = int(setup_port)
    except ValueError:
        return False
    if port < 1 or port > 65535:
        return False

    host_config = data.get("HostConfig")
    if not isinstance(host_config, dict):
        return False

    changed = host_config.get("NetworkMode") != "hassio"
    host_config["NetworkMode"] = "hassio"

    wanted_binding = [{"HostIp": "", "HostPort": str(port)}]
    port_bindings = host_config.setdefault("PortBindings", {})
    if not isinstance(port_bindings, dict):
        return changed
    if port_bindings.get("80/tcp") != wanted_binding:
        port_bindings["80/tcp"] = wanted_binding
        changed = True

    exposed_ports = data.setdefault("ExposedPorts", {})
    if isinstance(exposed_ports, dict) and "80/tcp" not in exposed_ports:
        exposed_ports["80/tcp"] = {}
        changed = True

    return changed


def _inject_env(data: dict[str, Any], name: str, value: str) -> bool:
    environment = data.get("Env")
    if environment is None:
        environment = []
        data["Env"] = environment
    if not isinstance(environment, list):
        return False

    prefix = f"{name}="
    index = next(
        (
            index
            for index, item in enumerate(environment)
            if isinstance(item, str) and item.startswith(prefix)
        ),
        None,
    )
    wanted = f"{name}={value}"
    if index is None:
        environment.append(wanted)
        return True
    if environment[index] != wanted:
        environment[index] = wanted
        return True
    return False


def _inject_udev_shim(data: dict[str, Any]) -> bool:
    changed = False
    host_config = data.setdefault("HostConfig", {})
    if not isinstance(host_config, dict):
        return False

    binds = host_config.get("Binds")
    if binds is None:
        binds = []
        host_config["Binds"] = binds
        changed = True
    if isinstance(binds, list) and not any(
        isinstance(bind, str) and bind.split(":", 2)[1:2] == [_UDEV_SHIM_PATH]
        for bind in binds
    ):
        binds.append(_UDEV_SHIM_BIND)
        changed = True

    environment = data.get("Env")
    if environment is None:
        environment = []
        data["Env"] = environment
        changed = True
    if not isinstance(environment, list):
        return changed

    pythonpath_index = next(
        (
            index
            for index, value in enumerate(environment)
            if isinstance(value, str) and value.startswith("PYTHONPATH=")
        ),
        None,
    )
    if pythonpath_index is None:
        environment.append(f"PYTHONPATH={_UDEV_SHIM_PATH}")
        changed = True
    else:
        current = environment[pythonpath_index].split("=", 1)[1]
        paths = current.split(":") if current else []
        if _UDEV_SHIM_PATH not in paths:
            environment[pythonpath_index] = (
                f"PYTHONPATH={_UDEV_SHIM_PATH}:{current}"
            ).rstrip(":")
            changed = True

    shim_index = next(
        (
            index
            for index, value in enumerate(environment)
            if isinstance(value, str) and value.startswith("USE_UDEV_SHIM=")
        ),
        None,
    )
    if shim_index is None:
        environment.append("USE_UDEV_SHIM=active")
        changed = True
    elif environment[shim_index] != "USE_UDEV_SHIM=active":
        environment[shim_index] = "USE_UDEV_SHIM=active"
        changed = True

    return changed


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
        warnings = data.get("Warnings")
        if isinstance(warnings, list):
            if _INFO_WARNING not in warnings:
                warnings.append(_INFO_WARNING)
        else:
            data["Warnings"] = [_INFO_WARNING]
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
