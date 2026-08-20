from __future__ import annotations

import json
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from haos_one_compat.docker_rules import (
    is_create_request,
    normalize_target_path,
    rewrite_create_request_payload,
    rewrite_json_payload,
    udev_shim_enabled,
    user_namespace_is_remapped,
)


class DockerRulesTests(unittest.TestCase):
    def test_normalize_target_path_handles_versioned_and_plain_endpoints(self) -> None:
        self.assertEqual(normalize_target_path("/info"), "/info")
        self.assertEqual(normalize_target_path("/v1.47/info"), "/info")
        self.assertEqual(normalize_target_path("/containers/json"), "/containers/json")
        self.assertEqual(
            normalize_target_path("/v1.47/containers/json?all=1"),
            "/containers/json",
        )
        self.assertIsNone(normalize_target_path("/version"))
        self.assertIsNone(normalize_target_path("/containers/abc/json"))

    def test_rewrite_info_adds_compat_warning_only(self) -> None:
        payload = json.dumps(
            {
                "OperatingSystem": "Home Assistant OS 17.1",
                "Architecture": "x86_64",
                "Driver": "overlay2",
                "Warnings": ["existing warning"],
            }
        ).encode("utf-8")

        rewritten = json.loads(rewrite_json_payload("/info", payload))

        self.assertEqual(
            rewritten["Warnings"],
            ["existing warning", "HAOS compat: intercepted"],
        )
        self.assertEqual(rewritten["OperatingSystem"], "Home Assistant OS 17.1")
        self.assertEqual(rewritten["Architecture"], "x86_64")
        self.assertEqual(rewritten["Driver"], "overlay2")

    def test_rewrite_containers_hides_compat_container(self) -> None:
        payload = json.dumps(
            [
                {"Id": "1", "Names": ["/haos_one_compat"], "Image": "haos_one_compat"},
                {"Id": "2", "Names": ["/hassio_supervisor"], "Image": "ghcr.io/home-assistant/amd64-hassio-supervisor:latest"},
            ]
        ).encode("utf-8")

        rewritten = json.loads(rewrite_json_payload("/v1.48/containers/json?all=1", payload))

        self.assertEqual(rewritten, [{"Id": "2", "Names": ["/hassio_supervisor"], "Image": "ghcr.io/home-assistant/amd64-hassio-supervisor:latest"}])

    def test_non_target_payload_is_unchanged(self) -> None:
        payload = b'{"Version":"29.1.3","Os":"linux","Arch":"amd64"}'
        self.assertEqual(rewrite_json_payload("/version", payload), payload)

    def test_recognizes_versioned_container_create_paths(self) -> None:
        self.assertTrue(is_create_request("/containers/create"))
        self.assertTrue(is_create_request("/v1.47/containers/create?name=test"))
        self.assertFalse(is_create_request("/containers/json"))
        self.assertFalse(is_create_request("/containers/test/start"))

    def test_create_request_removes_domainname_and_ulimits(self) -> None:
        payload = json.dumps(
            {
                "Image": "alpine:latest",
                "Hostname": "ha-test",
                "Domainname": "homeassistant",
                "HostConfig": {
                    "DnsSearch": ["homeassistant"],
                    "Ulimits": [{"Name": "nofile", "Soft": 1024, "Hard": 1024}],
                },
            }
        ).encode("utf-8")

        rewritten = json.loads(
            rewrite_create_request_payload(
                "/v1.47/containers/create?name=test",
                payload,
            )
        )

        self.assertNotIn("Domainname", rewritten)
        self.assertNotIn("Ulimits", rewritten["HostConfig"])
        self.assertEqual(rewritten["Hostname"], "ha-test")
        self.assertEqual(rewritten["HostConfig"]["DnsSearch"], ["homeassistant"])

    def test_create_request_without_compat_fields_is_unchanged(self) -> None:
        payload = b'{"Image":"alpine:latest","HostConfig":{"NetworkMode":"host"}}'
        self.assertIs(
            rewrite_create_request_payload("/containers/create", payload),
            payload,
        )

    def test_homeassistant_create_injects_setup_port(self) -> None:
        payload = json.dumps(
            {
                "Image": "ghcr.io/home-assistant/qemux86-64-homeassistant:2026.8.2",
                "Env": ["SUPERVISOR=http://supervisor"],
                "HostConfig": {"NetworkMode": "host"},
            }
        ).encode()

        rewritten = json.loads(
            rewrite_create_request_payload(
                "/v1.47/containers/create?name=homeassistant",
                payload,
                setup_port="8123",
            )
        )

        self.assertIn("SETUP_PORT=8123", rewritten["Env"])

    def test_landingpage_uses_hassio_network_and_publishes_setup_port(self) -> None:
        payload = json.dumps(
            {
                "Image": "ghcr.io/home-assistant/qemux86-64-homeassistant:landingpage",
                "Env": ["SUPERVISOR=172.30.32.2"],
                "HostConfig": {"NetworkMode": "host", "PortBindings": {}},
            }
        ).encode()

        rewritten = json.loads(
            rewrite_create_request_payload(
                "/containers/create?name=homeassistant",
                payload,
                setup_port="8765",
            )
        )

        self.assertEqual(rewritten["HostConfig"]["NetworkMode"], "hassio")
        self.assertEqual(
            rewritten["HostConfig"]["PortBindings"]["80/tcp"],
            [{"HostIp": "", "HostPort": "8765"}],
        )
        self.assertEqual(rewritten["ExposedPorts"]["80/tcp"], {})
        self.assertIn("SETUP_PORT=8765", rewritten["Env"])

    def test_core_keeps_host_network_with_setup_port(self) -> None:
        payload = json.dumps(
            {
                "Image": "ghcr.io/home-assistant/qemux86-64-homeassistant:2026.8.2",
                "HostConfig": {"NetworkMode": "host", "PortBindings": {}},
            }
        ).encode()

        rewritten = json.loads(
            rewrite_create_request_payload(
                "/containers/create?name=homeassistant",
                payload,
                setup_port="8765",
            )
        )

        self.assertEqual(rewritten["HostConfig"]["NetworkMode"], "host")
        self.assertEqual(rewritten["HostConfig"]["PortBindings"], {})
        self.assertIn("SETUP_PORT=8765", rewritten["Env"])

    def test_homeassistant_create_replaces_existing_setup_port(self) -> None:
        payload = b'{"Env":["SETUP_PORT=80","OTHER=value"],"HostConfig":{}}'
        rewritten = json.loads(
            rewrite_create_request_payload(
                "/containers/create?name=homeassistant",
                payload,
                setup_port="8123",
            )
        )
        self.assertEqual(rewritten["Env"], ["SETUP_PORT=8123", "OTHER=value"])

    def test_setup_port_is_not_injected_into_other_containers(self) -> None:
        payload = b'{"Image":"alpine:latest","HostConfig":{}}'
        self.assertIs(
            rewrite_create_request_payload(
                "/containers/create?name=addon_test",
                payload,
                setup_port="8123",
            ),
            payload,
        )

    def test_supervisor_create_injects_udev_shim(self) -> None:
        payload = json.dumps(
            {
                "Image": "ghcr.io/home-assistant/amd64-hassio-supervisor:latest",
                "Env": ["PYTHONPATH=/existing", "OTHER=value"],
                "HostConfig": {"Binds": ["/mnt/data:/data:rw"]},
            }
        ).encode()

        rewritten = json.loads(
            rewrite_create_request_payload(
                "/v1.47/containers/create?name=hassio_supervisor",
                payload,
                inject_udev_shim=True,
            )
        )

        self.assertIn(
            "/opt/haos-one-compat/udev-shim:/opt/haos-udev-shim:ro",
            rewritten["HostConfig"]["Binds"],
        )
        self.assertIn("PYTHONPATH=/opt/haos-udev-shim:/existing", rewritten["Env"])
        self.assertIn("USE_UDEV_SHIM=active", rewritten["Env"])
        self.assertIn("OTHER=value", rewritten["Env"])

    def test_udev_shim_is_not_injected_into_other_containers(self) -> None:
        payload = b'{"Image":"alpine:latest","HostConfig":{}}'

        rewritten = rewrite_create_request_payload(
            "/containers/create?name=homeassistant",
            payload,
            inject_udev_shim=True,
        )

        self.assertIs(rewritten, payload)

    def test_udev_shim_mode_resolution(self) -> None:
        remapped = "         0     100000      65536\n"
        identity = "         0          0 4294967295\n"

        self.assertTrue(user_namespace_is_remapped(remapped))
        self.assertFalse(user_namespace_is_remapped(identity))
        self.assertTrue(udev_shim_enabled("auto", remapped))
        self.assertFalse(udev_shim_enabled("auto", identity))
        self.assertTrue(udev_shim_enabled("force", identity))
        self.assertFalse(udev_shim_enabled("off", remapped))
        with self.assertRaisesRegex(ValueError, "USE_UDEV_SHIM"):
            udev_shim_enabled("invalid", remapped)

    def test_malformed_create_request_is_unchanged(self) -> None:
        payload = b"not-json"
        self.assertIs(
            rewrite_create_request_payload("/containers/create", payload),
            payload,
        )
