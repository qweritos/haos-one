"""Dataclass definitions for dummy NetworkManager state."""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any

from dbus_fast import Variant


@dataclass
class DeviceState:
    """State for a NetworkManager device."""

    object_path: str
    interface: str
    device_type: int
    driver: str
    managed: bool
    hw_address: str
    path: str
    active_connection: str


@dataclass
class ActiveConnectionState:
    """State for a NetworkManager active connection."""

    object_path: str
    connection: str
    connection_id: str
    connection_uuid: str
    connection_type: str
    state: int
    state_flags: int
    ip4_config: str
    ip6_config: str


@dataclass
class SettingsState:
    """State for a NetworkManager settings connection."""

    object_path: str
    settings: dict[str, dict[str, Variant]]


@dataclass
class IP4ConfigState:
    """State for an IP4Config object."""

    object_path: str
    address_data: list[dict[str, Variant]]
    gateway: str
    nameserver_data: list[dict[str, Variant]]


@dataclass
class IP6ConfigState:
    """State for an IP6Config object."""

    object_path: str
    address_data: list[dict[str, Variant]]
    gateway: str
    nameservers: list[bytes]


@dataclass
class AccessPointState:
    """State for an AccessPoint object."""

    object_path: str
    ssid: bytes
    frequency: int
    hw_address: str
    mode: int
    strength: int


@dataclass
class DummyState:
    """Shared state for all dummy interfaces."""

    version: str
    connectivity: int
    connectivity_check_enabled: bool
    primary_connection: str
    devices: dict[str, DeviceState] = field(default_factory=dict)
    active_connections: dict[str, ActiveConnectionState] = field(default_factory=dict)
    settings: dict[str, SettingsState] = field(default_factory=dict)
    ip4_configs: dict[str, IP4ConfigState] = field(default_factory=dict)
    ip6_configs: dict[str, IP6ConfigState] = field(default_factory=dict)
    access_points: dict[str, AccessPointState] = field(default_factory=dict)
    dns_configuration: list[dict[str, Variant]] = field(default_factory=list)
