"""Helper utilities for dummy NetworkManager service."""

from __future__ import annotations

from typing import Any

from dbus_fast import Variant
from dbus_fast.errors import DBusError


def v(signature: str, value: Any) -> Variant:
    """Create a DBus Variant."""
    return Variant(signature, value)


def _unwrap(value: Any) -> Any:
    """Unwrap Variant values if present."""
    if isinstance(value, Variant):
        return value.value
    return value


def forbid_modification() -> None:
    """Raise a D-Bus error for forbidden write attempts."""
    raise DBusError(
        "org.freedesktop.NetworkManager.Error.PermissionDenied",
        "Modifications are forbidden in dummy NetworkManager.",
    )
