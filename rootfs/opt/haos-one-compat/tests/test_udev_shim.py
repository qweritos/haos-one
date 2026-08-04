from __future__ import annotations

import runpy
import select
import sys
import types
import unittest
from pathlib import Path
from unittest.mock import patch


class _Monitor:
    @classmethod
    def from_netlink(cls, context, source="udev"):
        return (context, source)


class UdevShimTests(unittest.TestCase):
    def test_kernel_monitor_is_idle_and_other_sources_pass_through(self) -> None:
        pyudev = types.ModuleType("pyudev")
        pyudev.Monitor = _Monitor
        shim_path = Path(__file__).resolve().parents[1] / "udev-shim" / "sitecustomize.py"

        with patch.dict(sys.modules, {"pyudev": pyudev}):
            runpy.run_path(str(shim_path))
            monitor = pyudev.Monitor.from_netlink(object(), "kernel")

            self.assertEqual(pyudev.Monitor.from_netlink("context", "udev"), ("context", "udev"))
            self.assertEqual(monitor.poll(timeout=0), None)
            self.assertEqual(select.select([monitor], [], [], 0)[0], [])
            monitor.start()
            monitor.set_receive_buffer_size(1024)
            monitor.filter_by(subsystem="usb")
            monitor.filter_by_tag("systemd")
            monitor.remove_filter()


if __name__ == "__main__":
    unittest.main()
