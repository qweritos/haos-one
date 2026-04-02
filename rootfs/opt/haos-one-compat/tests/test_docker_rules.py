from __future__ import annotations

import json
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from haos_one_compat.docker_rules import normalize_target_path, rewrite_json_payload


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

    def test_rewrite_info_adds_compat_marker_only(self) -> None:
        payload = json.dumps(
            {
                "OperatingSystem": "Home Assistant OS 17.1",
                "Architecture": "x86_64",
                "Driver": "overlay2",
            }
        ).encode("utf-8")

        rewritten = json.loads(rewrite_json_payload("/info", payload))

        self.assertEqual(rewritten["HAOSCompat"], "intercepted")
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
