#!/usr/bin/env python3
"""Dummy NetworkManager D-Bus service for HASS Supervisor"""

from __future__ import annotations

import argparse
import asyncio
import json
import logging
import os
import time
from ipaddress import IPv6Address
from typing import Any
from uuid import uuid4

from dbus_fast import BusType, Variant
from dbus_fast.aio.message_bus import MessageBus
from dbus_fast.service import PropertyAccess, ServiceInterface, dbus_property, method, signal

_LOGGER = logging.getLogger(__name__)

_NETWORK_PROJECTION_PATH = "/host-run/haos-one-net/network.json"
_NETWORK_PROJECTION_MAX_AGE = 15

from .constants import (
    IP4_PATH,
    IP6_PATH,
    NM_BUS_NAME,
    NM_DNS_PATH,
    NM_ROOT_PATH,
    NM_SETTINGS_PATH,
)
from .utils import _unwrap, forbid_modification, v
from .types import (
    AccessPointState,
    ActiveConnectionState,
    DeviceState,
    DummyState,
    IP4ConfigState,
    IP6ConfigState,
    SettingsState,
)


class DummyRegistry:
    """Registry for exporting interfaces and managing state."""

    def __init__(
        self,
        bus: MessageBus,
        state: DummyState,
        projection_path: str = _NETWORK_PROJECTION_PATH,
    ) -> None:
        self.bus = bus
        self._state = state
        self._projection_path = projection_path
        self._settings_counter = 10
        self._active_counter = 10
        self._settings_services: dict[str, SettingsConnection] = {}
        self._active_services: dict[str, ActiveConnection] = {}

    @property
    def state(self) -> DummyState:
        """Return state projected from the active HAOS One tunnel, if any."""
        # Revalidate freshness on every D-Bus read so an agent crash cannot
        # leave a stale tunnel projected as the primary connection.
        self._state = build_state(load_network_projection(self._projection_path))
        return self._state

    def export(self, service: ServiceInterface, object_path: str) -> None:
        """Export a service interface on the bus."""
        self.bus.export(object_path, service)


class NetworkManager(ServiceInterface):
    """org.freedesktop.NetworkManager root object."""

    def __init__(self, registry: DummyRegistry) -> None:
        super().__init__("org.freedesktop.NetworkManager")
        self._registry = registry

    @dbus_property(access=PropertyAccess.READ)
    def Version(self) -> "s":
        return self._registry.state.version

    @dbus_property(access=PropertyAccess.READ)
    def Connectivity(self) -> "u":
        return self._registry.state.connectivity

    @dbus_property(access=PropertyAccess.READ)
    def ConnectivityCheckEnabled(self) -> "b":
        return self._registry.state.connectivity_check_enabled

    @dbus_property(access=PropertyAccess.READ)
    def Devices(self) -> "ao":
        return sorted(self._registry.state.devices.keys())

    @dbus_property(access=PropertyAccess.READ)
    def PrimaryConnection(self) -> "o":
        return self._registry.state.primary_connection

    @method()
    def ActivateConnection(self, connection: "o", device: "o", specific_object: "o") -> "o":
        forbid_modification()

    @method()
    def AddAndActivateConnection(
        self, settings: "a{sa{sv}}", device: "o", specific_object: "o"
    ) -> "oo":
        forbid_modification()

    @method()
    def CheckConnectivity(self) -> "u":
        return self._registry.state.connectivity


class Settings(ServiceInterface):
    """org.freedesktop.NetworkManager.Settings."""

    def __init__(self, registry: DummyRegistry) -> None:
        super().__init__("org.freedesktop.NetworkManager.Settings")
        self._registry = registry

    @dbus_property(access=PropertyAccess.READ)
    def Connections(self) -> "ao":
        return sorted(self._registry.state.settings.keys())

    @method()
    def AddConnection(self, connection: "a{sa{sv}}") -> "o":
        forbid_modification()

    @method()
    def ReloadConnections(self) -> "b":
        forbid_modification()


class SettingsConnection(ServiceInterface):
    """org.freedesktop.NetworkManager.Settings.Connection."""

    def __init__(self, registry: DummyRegistry, object_path: str) -> None:
        super().__init__("org.freedesktop.NetworkManager.Settings.Connection")
        self._registry = registry
        self.object_path = object_path

    @signal()
    def Updated(self) -> "":
        """Signal Updated."""
        return []

    @signal()
    def Removed(self) -> "":
        """Signal Removed."""
        return []

    @method()
    def GetSettings(self) -> "a{sa{sv}}":
        return self._registry.state.settings[self.object_path].settings

    @method()
    def Update(self, properties: "a{sa{sv}}") -> "":
        forbid_modification()

    @method()
    def Delete(self) -> "":
        forbid_modification()


class Device(ServiceInterface):
    """org.freedesktop.NetworkManager.Device."""

    def __init__(self, registry: DummyRegistry, object_path: str) -> None:
        super().__init__("org.freedesktop.NetworkManager.Device")
        self._registry = registry
        self.object_path = object_path

    @property
    def _state(self) -> DeviceState:
        return self._registry.state.devices[self.object_path]

    @dbus_property(access=PropertyAccess.READ)
    def Interface(self) -> "s":
        return self._state.interface

    @dbus_property(access=PropertyAccess.READ)
    def DeviceType(self) -> "u":
        return self._state.device_type

    @dbus_property(access=PropertyAccess.READ)
    def Driver(self) -> "s":
        return self._state.driver

    @dbus_property(access=PropertyAccess.READ)
    def Managed(self) -> "b":
        return self._state.managed

    @dbus_property(access=PropertyAccess.READ)
    def HwAddress(self) -> "s":
        return self._state.hw_address

    @dbus_property(access=PropertyAccess.READ)
    def Path(self) -> "s":
        return self._state.path

    @dbus_property(access=PropertyAccess.READ)
    def ActiveConnection(self) -> "o":
        return self._state.active_connection


class DeviceWireless(ServiceInterface):
    """org.freedesktop.NetworkManager.Device.Wireless."""

    def __init__(self, registry: DummyRegistry, object_path: str, access_points: list[str]) -> None:
        super().__init__("org.freedesktop.NetworkManager.Device.Wireless")
        self._registry = registry
        self.object_path = object_path
        self._access_points = access_points

    @dbus_property(access=PropertyAccess.READ)
    def Bitrate(self) -> "u":
        return 0

    @dbus_property(access=PropertyAccess.READ)
    def ActiveAccessPoint(self) -> "o":
        return "/"

    @method()
    def RequestScan(self, options: "a{sv}") -> "":
        forbid_modification()

    @method()
    def GetAllAccessPoints(self) -> "ao":
        return self._access_points


class ActiveConnection(ServiceInterface):
    """org.freedesktop.NetworkManager.Connection.Active."""

    def __init__(self, registry: DummyRegistry, object_path: str) -> None:
        super().__init__("org.freedesktop.NetworkManager.Connection.Active")
        self._registry = registry
        self.object_path = object_path

    @property
    def _state(self) -> ActiveConnectionState:
        return self._registry.state.active_connections[self.object_path]

    @signal()
    def StateChanged(self) -> "uu":
        return [self._state.state, 0]

    @dbus_property(access=PropertyAccess.READ)
    def Connection(self) -> "o":
        return self._state.connection

    @dbus_property(access=PropertyAccess.READ)
    def Id(self) -> "s":
        return self._state.connection_id

    @dbus_property(access=PropertyAccess.READ)
    def Uuid(self) -> "s":
        return self._state.connection_uuid

    @dbus_property(access=PropertyAccess.READ)
    def Type(self) -> "s":
        return self._state.connection_type

    @dbus_property(access=PropertyAccess.READ)
    def State(self) -> "u":
        return self._state.state

    @dbus_property(access=PropertyAccess.READ)
    def StateFlags(self) -> "u":
        return self._state.state_flags

    @dbus_property(access=PropertyAccess.READ)
    def Ip4Config(self) -> "o":
        return self._state.ip4_config

    @dbus_property(access=PropertyAccess.READ)
    def Ip6Config(self) -> "o":
        return self._state.ip6_config


class IP4Config(ServiceInterface):
    """org.freedesktop.NetworkManager.IP4Config."""

    def __init__(self, registry: DummyRegistry, object_path: str) -> None:
        super().__init__("org.freedesktop.NetworkManager.IP4Config")
        self._registry = registry
        self.object_path = object_path

    @property
    def _state(self) -> IP4ConfigState:
        return self._registry.state.ip4_configs[self.object_path]

    @dbus_property(access=PropertyAccess.READ)
    def AddressData(self) -> "aa{sv}":
        return self._state.address_data

    @dbus_property(access=PropertyAccess.READ)
    def Gateway(self) -> "s":
        return self._state.gateway

    @dbus_property(access=PropertyAccess.READ)
    def NameserverData(self) -> "aa{sv}":
        return self._state.nameserver_data


class IP6Config(ServiceInterface):
    """org.freedesktop.NetworkManager.IP6Config."""

    def __init__(self, registry: DummyRegistry, object_path: str) -> None:
        super().__init__("org.freedesktop.NetworkManager.IP6Config")
        self._registry = registry
        self.object_path = object_path

    @property
    def _state(self) -> IP6ConfigState:
        return self._registry.state.ip6_configs[self.object_path]

    @dbus_property(access=PropertyAccess.READ)
    def AddressData(self) -> "aa{sv}":
        return self._state.address_data

    @dbus_property(access=PropertyAccess.READ)
    def Gateway(self) -> "s":
        return self._state.gateway

    @dbus_property(access=PropertyAccess.READ)
    def Nameservers(self) -> "aay":
        return self._state.nameservers


class AccessPoint(ServiceInterface):
    """org.freedesktop.NetworkManager.AccessPoint."""

    def __init__(self, registry: DummyRegistry, object_path: str) -> None:
        super().__init__("org.freedesktop.NetworkManager.AccessPoint")
        self._registry = registry
        self.object_path = object_path

    @property
    def _state(self) -> AccessPointState:
        return self._registry.state.access_points[self.object_path]

    @dbus_property(access=PropertyAccess.READ)
    def Ssid(self) -> "ay":
        return self._state.ssid

    @dbus_property(access=PropertyAccess.READ)
    def Frequency(self) -> "u":
        return self._state.frequency

    @dbus_property(access=PropertyAccess.READ)
    def HwAddress(self) -> "s":
        return self._state.hw_address

    @dbus_property(access=PropertyAccess.READ)
    def Mode(self) -> "u":
        return self._state.mode

    @dbus_property(access=PropertyAccess.READ)
    def Strength(self) -> "y":
        return self._state.strength


class DnsManager(ServiceInterface):
    """org.freedesktop.NetworkManager.DnsManager."""

    def __init__(self, registry: DummyRegistry) -> None:
        super().__init__("org.freedesktop.NetworkManager.DnsManager")
        self._registry = registry

    @dbus_property(access=PropertyAccess.READ)
    def Mode(self) -> "s":
        return "default"

    @dbus_property(access=PropertyAccess.READ)
    def RcManager(self) -> "s":
        return "file"

    @dbus_property(access=PropertyAccess.READ)
    def Configuration(self) -> "aa{sv}":
        return self._registry.state.dns_configuration


def load_network_projection(path: str = _NETWORK_PROJECTION_PATH) -> dict[str, Any] | None:
    """Load a fresh, active haoswg0 projection produced by the guest agent."""
    try:
        with open(path, encoding="utf-8") as projection_file:
            projection = json.load(projection_file)
        if projection.get("version") != 1 or projection.get("interface") != "haoswg0":
            return None
        if time.time() - int(projection["updated_unix"]) > _NETWORK_PROJECTION_MAX_AGE:
            return None
        address = str(projection["address"])
        prefix = int(projection["prefix"])
        gateway = str(projection["gateway"])
        if not 0 <= prefix <= 32:
            return None
        return {
            "interface": "haoswg0",
            "address": address,
            "prefix": prefix,
            "gateway": gateway,
            "nameservers": [str(value) for value in projection.get("nameservers", [])],
        }
    except (OSError, ValueError, KeyError, TypeError, json.JSONDecodeError):
        return None


def _initial_settings_eth(profile: dict[str, Any]) -> dict[str, dict[str, Variant]]:
    return {
        "connection": {
            "id": v("s", f"Supervisor {profile['interface']}"),
            "uuid": v("s", "00000000-0000-0000-0000-000000000001"),
            "type": v("s", "802-3-ethernet"),
            "interface-name": v("s", profile["interface"]),
            "llmnr": v("i", 2),
            "mdns": v("i", 2),
        },
        "ipv4": {
            "method": v("s", "auto"),
            "address-data": v(
                "aa{sv}",
                [{"address": v("s", profile["address"]), "prefix": v("u", profile["prefix"])}],
            ),
            "gateway": v("s", profile["gateway"]),
        },
        "ipv6": {
            "method": v("s", "auto"),
            "addr-gen-mode": v("i", 0),
            "ip6-privacy": v("i", 0),
        },
        "802-3-ethernet": {
            "assigned-mac-address": v("s", "preserve"),
        },
    }



def build_state(projection: dict[str, Any] | None = None) -> DummyState:
    """Build an initial dummy state."""
    profile = projection or {
        "interface": "eth0",
        "address": "192.168.1.100",
        "prefix": 24,
        "gateway": "192.168.1.1",
        "nameservers": ["192.168.1.1"],
    }
    devices = {
        "/org/freedesktop/NetworkManager/Devices/1": DeviceState(
            object_path="/org/freedesktop/NetworkManager/Devices/1",
            interface=profile["interface"],
            device_type=1,
            # Supervisor expects the compatibility device to be Ethernet. The
            # interface identity and IPv4 data may describe haoswg0, but its
            # NetworkManager surface deliberately remains fake Ethernet.
            driver="dummy-eth",
            managed=True,
            hw_address="AA:BB:CC:DD:EE:01",
            path=f"dummy-{profile['interface']}",
            active_connection="/org/freedesktop/NetworkManager/ActiveConnection/1",
        ),
    }

    active_connections = {
        "/org/freedesktop/NetworkManager/ActiveConnection/1": ActiveConnectionState(
            object_path="/org/freedesktop/NetworkManager/ActiveConnection/1",
            connection="/org/freedesktop/NetworkManager/Settings/1",
            connection_id=f"Supervisor {profile['interface']}",
            connection_uuid="00000000-0000-0000-0000-000000000001",
            connection_type="802-3-ethernet",
            state=2,
            state_flags=0,
            ip4_config=IP4_PATH,
            ip6_config=IP6_PATH,
        )
    }

    settings = {
        "/org/freedesktop/NetworkManager/Settings/1": SettingsState(
            object_path="/org/freedesktop/NetworkManager/Settings/1",
            settings=_initial_settings_eth(profile),
        ),
    }

    ip4_configs = {
        IP4_PATH: IP4ConfigState(
            object_path=IP4_PATH,
            address_data=[{"address": v("s", profile["address"]), "prefix": v("u", profile["prefix"])}],
            gateway=profile["gateway"],
            nameserver_data=[{"address": v("s", address)} for address in profile["nameservers"]],
        )
    }

    ip6_configs = {
        IP6_PATH: IP6ConfigState(
            object_path=IP6_PATH,
            address_data=[
                {"address": v("s", "2001:db8::100"), "prefix": v("u", 64)}
            ],
            gateway="fe80::1",
            nameservers=[IPv6Address("2001:4860:4860::8888").packed],
        )
    }

    dns_configuration = [
        {
            "nameservers": v("as", profile["nameservers"]),
            "domains": v("as", ["local"]),
            "interface": v("s", profile["interface"]),
            "priority": v("i", 100),
            "vpn": v("b", False),
        }
    ]

    return DummyState(
        version="1.42.0",
        connectivity=4,
        connectivity_check_enabled=True,
        primary_connection="/org/freedesktop/NetworkManager/ActiveConnection/1",
        devices=devices,
        active_connections=active_connections,
        settings=settings,
        ip4_configs=ip4_configs,
        ip6_configs=ip6_configs,
        dns_configuration=dns_configuration,
    )


def build_arg_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="Dummy NetworkManager D-Bus service")
    parser.add_argument(
        "--bus",
        choices=["session", "system"],
        default="session",
        help="Bus type to connect to (default: session)",
    )
    parser.add_argument(
        "--verbose", action="store_true", help="Enable debug logging"
    )
    return parser


async def run_service(args: argparse.Namespace) -> None:
    bus_type = BusType.SYSTEM if args.bus == "system" else BusType.SESSION
    bus = await MessageBus(bus_type=bus_type).connect()
    await bus.request_name(NM_BUS_NAME)

    state = build_state()
    registry = DummyRegistry(bus, state)

    # Root interfaces
    registry.export(NetworkManager(registry), NM_ROOT_PATH)
    registry.export(Settings(registry), NM_SETTINGS_PATH)
    registry.export(DnsManager(registry), NM_DNS_PATH)

    # Devices
    for device_path in state.devices:
        registry.export(Device(registry, device_path), device_path)

    # Active connections
    for active_path in state.active_connections:
        registry.export(ActiveConnection(registry, active_path), active_path)

    # Settings connections
    for settings_path in state.settings:
        registry.export(SettingsConnection(registry, settings_path), settings_path)

    # IP configs
    for ip4_path in state.ip4_configs:
        registry.export(IP4Config(registry, ip4_path), ip4_path)
    for ip6_path in state.ip6_configs:
        registry.export(IP6Config(registry, ip6_path), ip6_path)

    _LOGGER.info("Dummy NetworkManager service running on %s bus", args.bus)
    await asyncio.Event().wait()


def main() -> None:
    parser = build_arg_parser()
    args = parser.parse_args()

    logging.basicConfig(
        level=logging.DEBUG if args.verbose else logging.INFO,
        format="%(asctime)s %(levelname)s %(name)s: %(message)s",
    )

    asyncio.run(run_service(args))


if __name__ == "__main__":
    main()
