"""Tests for the one-time Supervisor udev shim migration."""

from __future__ import annotations

import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest


SCRIPT = (
    Path(__file__).parents[3]
    / "usr"
    / "local"
    / "bin"
    / "haos-supervisor-udev-shim-migrate"
)
SHIM_PATH = "/opt/haos-udev-shim"


def supervisor_inspect(*, configured: bool) -> str:
    environment = ["PATH=/usr/bin"]
    mounts = []
    if configured:
        environment.extend(
            [f"PYTHONPATH={SHIM_PATH}", "USE_UDEV_SHIM=active"]
        )
        mounts.append(
            {
                "Source": "/opt/haos-one-compat/udev-shim",
                "Destination": SHIM_PATH,
                "RW": False,
            }
        )
    return json.dumps([{"Config": {"Env": environment}, "Mounts": mounts}])


class SupervisorMigrationTests(unittest.TestCase):
    def run_migration(
        self,
        *,
        mode: str,
        uid_map: str,
        configured: bool = False,
        container_exists: bool = True,
    ) -> list[str]:
        with tempfile.TemporaryDirectory() as temporary_directory:
            root = Path(temporary_directory)
            mode_file = root / "mode"
            uid_map_file = root / "uid_map"
            inspect_file = root / "inspect.json"
            docker_log = root / "docker.log"
            docker_bin = root / "docker"

            mode_file.write_text(f"{mode}\n", encoding="ascii")
            uid_map_file.write_text(uid_map, encoding="ascii")
            inspect_file.write_text(
                supervisor_inspect(configured=configured), encoding="utf-8"
            )
            docker_bin.write_text(
                """#!/bin/sh
printf '%s\\n' "$*" >> "$DOCKER_LOG"
if [ "$1 $2 $3" = "container inspect hassio_supervisor" ]; then
  if [ "${CONTAINER_EXISTS:-1}" = 1 ]; then
    cat "$INSPECT_FILE"
    exit 0
  fi
  echo "Error: No such container: hassio_supervisor" >&2
  exit 1
fi
if [ "$1 $2 $3 $4" = "container rm -f hassio_supervisor" ]; then
  exit 0
fi
exit 2
""",
                encoding="ascii",
            )
            docker_bin.chmod(0o755)

            environment = os.environ.copy()
            environment.update(
                {
                    "CONTAINER_EXISTS": "1" if container_exists else "0",
                    "DOCKER_BIN": str(docker_bin),
                    "DOCKER_LOG": str(docker_log),
                    "INSPECT_FILE": str(inspect_file),
                    "UDEV_SHIM_MODE_FILE": str(mode_file),
                    "UID_MAP_FILE": str(uid_map_file),
                }
            )
            subprocess.run([str(SCRIPT)], check=True, env=environment)
            if not docker_log.exists():
                return []
            return docker_log.read_text(encoding="utf-8").splitlines()

    def test_auto_recreates_unconfigured_supervisor_when_root_is_remapped(self):
        calls = self.run_migration(mode="auto", uid_map="0 100000 65536\n")
        self.assertEqual(
            calls,
            [
                "container inspect hassio_supervisor",
                "container rm -f hassio_supervisor",
            ],
        )

    def test_auto_keeps_configured_supervisor(self):
        calls = self.run_migration(
            mode="auto",
            uid_map="0 100000 65536\n",
            configured=True,
        )
        self.assertEqual(calls, ["container inspect hassio_supervisor"])

    def test_auto_is_noop_without_user_namespace_remap(self):
        calls = self.run_migration(mode="auto", uid_map="0 0 4294967295\n")
        self.assertEqual(calls, [])

    def test_force_recreates_without_user_namespace_remap(self):
        calls = self.run_migration(mode="force", uid_map="0 0 4294967295\n")
        self.assertEqual(calls[-1], "container rm -f hassio_supervisor")

    def test_off_is_noop(self):
        calls = self.run_migration(mode="off", uid_map="0 100000 65536\n")
        self.assertEqual(calls, [])

    def test_missing_supervisor_is_noop(self):
        calls = self.run_migration(
            mode="force",
            uid_map="0 0 4294967295\n",
            container_exists=False,
        )
        self.assertEqual(calls, ["container inspect hassio_supervisor"])


if __name__ == "__main__":
    unittest.main()
