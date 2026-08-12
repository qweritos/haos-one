"""Provide an idle pyudev kernel monitor when nested LXC blocks uevents."""

from __future__ import annotations

import logging
import os

import pyudev

_LOGGER = logging.getLogger("haos_udev_shim")
_ORIGINAL_FROM_NETLINK = pyudev.Monitor.from_netlink


class _IdleMonitor:
    """Pollable pyudev monitor that deliberately produces no hotplug events."""

    def __init__(self, context) -> None:
        self.context = context
        self._read_fd, self._write_fd = os.pipe()
        os.set_blocking(self._read_fd, False)
        os.set_blocking(self._write_fd, False)

    def fileno(self) -> int:
        return self._read_fd

    def start(self) -> None:
        return None

    def poll(self, timeout=None):
        return None

    def set_receive_buffer_size(self, size: int) -> None:
        return None

    def filter_by(self, subsystem=None, device_type=None) -> None:
        return None

    def filter_by_tag(self, tag: str) -> None:
        return None

    def remove_filter(self) -> None:
        return None


def _from_netlink(cls, context, source="udev"):
    if source != "udev" or os.getenv("USE_UDEV_SHIM") != "active":
        return _ORIGINAL_FROM_NETLINK(context, source)
    _LOGGER.info("Using idle kernel udev monitor for nested LXC compatibility")
    return _IdleMonitor(context)


pyudev.Monitor.from_netlink = classmethod(_from_netlink)
