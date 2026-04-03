from __future__ import annotations

import asyncio
import contextlib
import json
import os
import stat
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from haos_one_compat.docker_proxy import DockerSocketProxy


class DockerProxyTests(unittest.IsolatedAsyncioTestCase):
    async def asyncSetUp(self) -> None:
        self.tempdir = tempfile.TemporaryDirectory()
        self.root = Path(self.tempdir.name)
        self.upstream_path = self.root / "docker-real.sock"
        self.frontend_path = self.root / "docker.sock"
        self.requests: list[str] = []
        self.upstream_server = await asyncio.start_unix_server(
            self._handle_upstream,
            path=str(self.upstream_path),
        )
        self.proxy = DockerSocketProxy(
            frontend_path=str(self.frontend_path),
            upstream_path=str(self.upstream_path),
        )
        await self.proxy.start()

    async def asyncTearDown(self) -> None:
        await self.proxy.close()
        self.upstream_server.close()
        await self.upstream_server.wait_closed()
        self.tempdir.cleanup()

    async def _handle_upstream(
        self,
        reader: asyncio.StreamReader,
        writer: asyncio.StreamWriter,
    ) -> None:
        try:
            while True:
                try:
                    header = await reader.readuntil(b"\r\n\r\n")
                except asyncio.IncompleteReadError:
                    return

                request_line = header.split(b"\r\n", 1)[0].decode("ascii")
                _, path, _ = request_line.split(" ", 2)
                self.requests.append(path)

                if path == "/containers/json?all=1":
                    payload = json.dumps(
                        [
                            {"Id": "1", "Names": ["/haos_one_compat"], "Image": "haos_one_compat"},
                            {"Id": "2", "Names": ["/kept"], "Image": "busybox:latest"},
                        ]
                    ).encode("utf-8")
                elif path == "/info":
                    payload = b'{"unchanged":true}'
                    writer.write(
                        b"HTTP/1.1 200 OK\r\n"
                        b"Content-Type: application/json\r\n"
                        b"Transfer-Encoding: chunked\r\n\r\n"
                    )
                    writer.write(f"{len(payload):X}\r\n".encode("ascii"))
                    writer.write(payload + b"\r\n0\r\n\r\n")
                    await writer.drain()
                    continue
                else:
                    payload = b'{"unchanged":true}'

                writer.write(
                    b"HTTP/1.1 200 OK\r\n"
                    b"Content-Type: application/json\r\n"
                    + f"Content-Length: {len(payload)}\r\n\r\n".encode("ascii")
                    + payload
                )
                await writer.drain()
        finally:
            writer.close()
            await writer.wait_closed()

    async def _request(self, path: str) -> tuple[str, bytes]:
        reader, writer = await asyncio.open_unix_connection(str(self.frontend_path))
        writer.write(
            f"GET {path} HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n".encode("ascii")
        )
        await writer.drain()
        raw = await reader.read()
        writer.close()
        await writer.wait_closed()
        header, body = raw.split(b"\r\n\r\n", 1)
        return header.decode("iso-8859-1"), body

    async def test_info_response_has_compat_warning(self) -> None:
        header, body = await self._request("/info")

        self.assertIn("Content-Length", header)
        self.assertNotIn("Transfer-Encoding", header)
        self.assertEqual(
            json.loads(body),
            {"unchanged": True, "Warnings": ["HAOS compat: intercepted"]},
        )

    async def test_passthrough_for_non_target_endpoint(self) -> None:
        header, body = await self._request("/containers/json")

        self.assertIn("Content-Length", header)
        self.assertEqual(json.loads(body), {"unchanged": True})
        self.assertEqual(self.requests[-1], "/containers/json")

    async def test_filters_compat_from_container_list(self) -> None:
        header, body = await self._request("/containers/json?all=1")

        self.assertIn("Content-Length", header)
        self.assertEqual(
            json.loads(body),
            [{"Id": "2", "Names": ["/kept"], "Image": "busybox:latest"}],
        )

    async def test_keep_alive_ping_then_container_list_still_rewrites(self) -> None:
        reader, writer = await asyncio.open_unix_connection(str(self.frontend_path))
        writer.write(
            b"HEAD /_ping HTTP/1.1\r\nHost: localhost\r\n"
            b"Connection: keep-alive\r\n\r\n"
            b"GET /containers/json?all=1 HTTP/1.1\r\nHost: localhost\r\n"
            b"Connection: close\r\n\r\n"
        )
        await writer.drain()
        raw = await reader.read()
        writer.close()
        await writer.wait_closed()

        responses = raw.split(b"HTTP/1.1 ")[1:]
        self.assertEqual(len(responses), 2)
        self.assertIn(b"200 OK\r\n", responses[0])
        body = responses[1].split(b"\r\n\r\n", 1)[1]
        self.assertEqual(
            json.loads(body),
            [{"Id": "2", "Names": ["/kept"], "Image": "busybox:latest"}],
        )

    async def test_replaces_stale_frontend_socket_path(self) -> None:
        await self.proxy.close()
        with contextlib.suppress(FileNotFoundError):
            os.unlink(self.frontend_path)
        self.frontend_path.write_text("stale")

        self.proxy = DockerSocketProxy(
            frontend_path=str(self.frontend_path),
            upstream_path=str(self.upstream_path),
        )
        await self.proxy.start()

        mode = os.stat(self.frontend_path).st_mode
        self.assertTrue(stat.S_ISSOCK(mode))
        upstream_gid = os.stat(self.upstream_path).st_gid
        self.assertEqual(os.stat(self.frontend_path).st_gid, upstream_gid)
