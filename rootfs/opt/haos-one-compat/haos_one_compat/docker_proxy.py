"""UNIX socket proxy for Docker API interception."""

from __future__ import annotations

import asyncio
import contextlib
import logging
import os
import stat
from dataclasses import dataclass
from email.parser import Parser
from typing import Iterable

from .docker_rules import (
    is_create_request,
    is_rewrite_target,
    rewrite_create_request_payload,
    rewrite_json_payload,
)

_LOGGER = logging.getLogger(__name__)
_HTTP_HEADER_LIMIT = 1024 * 1024
_READ_CHUNK_SIZE = 64 * 1024
_TUNNEL_PATH_MARKERS = (
    "/attach",
    "/events",
    "/exec/",
    "/logs",
    "/stats",
)


@dataclass(slots=True)
class HTTPRequestHead:
    method: str
    path: str
    version: str
    headers: list[tuple[str, str]]
    raw: bytes

    def header(self, name: str) -> str | None:
        target = name.lower()
        for key, value in self.headers:
            if key.lower() == target:
                return value
        return None


@dataclass(slots=True)
class HTTPResponse:
    version: str
    status_code: int
    reason: str
    headers: list[tuple[str, str]]
    body: bytes
    raw: bytes

    def header(self, name: str) -> str | None:
        target = name.lower()
        for key, value in self.headers:
            if key.lower() == target:
                return value
        return None


async def _read_header_block(reader: asyncio.StreamReader) -> bytes:
    header = bytearray()
    while b"\r\n\r\n" not in header:
        chunk = await reader.readuntil(b"\r\n")
        header.extend(chunk)
        if len(header) > _HTTP_HEADER_LIMIT:
            raise ValueError("HTTP header too large")
    return bytes(header)


def _parse_headers(header_bytes: bytes) -> list[tuple[str, str]]:
    text = header_bytes.decode("iso-8859-1")
    parsed = Parser().parsestr(text)
    return list(parsed.items())


async def _read_request_head(reader: asyncio.StreamReader) -> HTTPRequestHead:
    raw = await _read_header_block(reader)
    start_line, header_blob = raw.split(b"\r\n", 1)
    method, path, version = start_line.decode("iso-8859-1").split(" ", 2)
    return HTTPRequestHead(
        method=method,
        path=path,
        version=version,
        headers=_parse_headers(header_blob),
        raw=raw,
    )


async def _read_chunked_body(
    reader: asyncio.StreamReader,
) -> tuple[bytes, bytes]:
    decoded = bytearray()
    raw = bytearray()

    while True:
        size_line = await reader.readuntil(b"\r\n")
        raw.extend(size_line)
        size = int(size_line.split(b";", 1)[0].strip(), 16)
        if size == 0:
            while True:
                trailer = await reader.readuntil(b"\r\n")
                raw.extend(trailer)
                if trailer == b"\r\n":
                    return bytes(decoded), bytes(raw)
        chunk = await reader.readexactly(size)
        decoded.extend(chunk)
        raw.extend(chunk)
        ending = await reader.readexactly(2)
        raw.extend(ending)


async def _forward_request_body(
    request: HTTPRequestHead,
    reader: asyncio.StreamReader,
    writer: asyncio.StreamWriter,
) -> None:
    content_length = request.header("Content-Length")
    transfer_encoding = request.header("Transfer-Encoding")

    if content_length:
        remaining = int(content_length)
        while remaining > 0:
            chunk = await reader.read(min(_READ_CHUNK_SIZE, remaining))
            if not chunk:
                raise ConnectionError("client closed while sending request body")
            remaining -= len(chunk)
            writer.write(chunk)
            await writer.drain()
        return

    if transfer_encoding and "chunked" in transfer_encoding.lower():
        _, raw = await _read_chunked_body(reader)
        writer.write(raw)
        await writer.drain()


async def _read_request_body(
    request: HTTPRequestHead,
    reader: asyncio.StreamReader,
) -> tuple[bytes, bytes]:
    """Read a request body and return its decoded and original representations."""
    content_length = request.header("Content-Length")
    transfer_encoding = request.header("Transfer-Encoding")

    if content_length:
        body = await reader.readexactly(int(content_length))
        return body, body

    if transfer_encoding and "chunked" in transfer_encoding.lower():
        return await _read_chunked_body(reader)

    return b"", b""


async def _read_response(
    reader: asyncio.StreamReader,
    request_method: str,
) -> HTTPResponse:
    raw_header = await _read_header_block(reader)
    start_line, header_blob = raw_header.split(b"\r\n", 1)
    version, status_code, reason = start_line.decode("iso-8859-1").split(" ", 2)
    headers = _parse_headers(header_blob)
    response = HTTPResponse(
        version=version,
        status_code=int(status_code),
        reason=reason,
        headers=headers,
        body=b"",
        raw=raw_header,
    )

    if request_method == "HEAD" or response.status_code in (204, 304):
        return response

    transfer_encoding = response.header("Transfer-Encoding")
    content_length = response.header("Content-Length")

    if transfer_encoding and "chunked" in transfer_encoding.lower():
        body, raw_body = await _read_chunked_body(reader)
        response.body = body
        response.raw += raw_body
        return response

    if content_length:
        body = await reader.readexactly(int(content_length))
        response.body = body
        response.raw += body
        return response

    body = await reader.read()
    response.body = body
    response.raw += body
    return response


def _serialize_headers(headers: Iterable[tuple[str, str]]) -> bytes:
    lines = [f"{key}: {value}".encode("iso-8859-1") for key, value in headers]
    return b"\r\n".join(lines)


def rewrite_http_request(request: HTTPRequestHead, body: bytes) -> bytes:
    """Serialize a container-create request with compatibility options removed."""
    rewritten_body = rewrite_create_request_payload(request.path, body)
    if rewritten_body == body:
        return request.raw + body

    headers = [
        (key, value)
        for key, value in request.headers
        if key.lower() not in {"content-length", "transfer-encoding", "trailer"}
    ]
    headers.append(("Content-Length", str(len(rewritten_body))))
    request_line = f"{request.method} {request.path} {request.version}".encode(
        "iso-8859-1"
    )
    header_blob = _serialize_headers(headers)
    parts = [request_line, b"\r\n"]
    if header_blob:
        parts.extend((header_blob, b"\r\n"))
    parts.extend((b"\r\n", rewritten_body))
    return b"".join(parts)


def rewrite_http_response(path: str, response: HTTPResponse) -> bytes:
    """Return either the original raw response or a rewritten JSON response."""
    content_type = response.header("Content-Type") or ""
    content_encoding = response.header("Content-Encoding") or ""

    if response.status_code < 200 or response.status_code >= 300:
        return response.raw
    if "json" not in content_type.lower():
        return response.raw
    if content_encoding and content_encoding.lower() != "identity":
        return response.raw

    rewritten_body = rewrite_json_payload(path, response.body)
    if rewritten_body == response.body:
        return response.raw

    headers = [
        (key, value)
        for key, value in response.headers
        if key.lower() not in {"content-length", "transfer-encoding"}
    ]
    headers.append(("Content-Length", str(len(rewritten_body))))
    status_line = (
        f"{response.version} {response.status_code} {response.reason}".encode("iso-8859-1")
    )
    header_blob = _serialize_headers(headers)
    parts = [status_line, b"\r\n"]
    if header_blob:
        parts.extend((header_blob, b"\r\n"))
    parts.extend((b"\r\n", rewritten_body))
    return b"".join(parts)


def _header_contains(headers: list[tuple[str, str]], name: str, token: str) -> bool:
    target = name.lower()
    expected = token.lower()
    for key, value in headers:
        if key.lower() != target:
            continue
        if expected in value.lower():
            return True
    return False


def _request_wants_tunnel(request: HTTPRequestHead) -> bool:
    if _header_contains(request.headers, "Connection", "upgrade"):
        return True
    path = request.path.lower()
    return any(marker in path for marker in _TUNNEL_PATH_MARKERS)


def _uses_keep_alive(version: str, headers: list[tuple[str, str]]) -> bool:
    connection_close = _header_contains(headers, "Connection", "close")
    connection_keepalive = _header_contains(headers, "Connection", "keep-alive")
    if version == "HTTP/1.0":
        return connection_keepalive
    return not connection_close


async def _pipe(
    reader: asyncio.StreamReader,
    writer: asyncio.StreamWriter,
) -> None:
    while True:
        chunk = await reader.read(_READ_CHUNK_SIZE)
        if not chunk:
            break
        writer.write(chunk)
        await writer.drain()


async def _bidirectional_relay(
    client_reader: asyncio.StreamReader,
    client_writer: asyncio.StreamWriter,
    upstream_reader: asyncio.StreamReader,
    upstream_writer: asyncio.StreamWriter,
) -> None:
    tasks = {
        asyncio.create_task(_pipe(client_reader, upstream_writer)),
        asyncio.create_task(_pipe(upstream_reader, client_writer)),
    }
    done, pending = await asyncio.wait(tasks, return_when=asyncio.FIRST_COMPLETED)
    for task in pending:
        task.cancel()
    for task in pending:
        with contextlib.suppress(asyncio.CancelledError):
            await task
    for task in done:
        with contextlib.suppress(ConnectionError, BrokenPipeError):
            task.result()


class DockerSocketProxy:
    """Expose the intercepted Docker API on a frontend UNIX socket."""

    def __init__(self, frontend_path: str, upstream_path: str) -> None:
        self.frontend_path = frontend_path
        self.upstream_path = upstream_path
        self._server: asyncio.AbstractServer | None = None

    async def _wait_for_upstream(self, timeout: float = 30.0) -> None:
        deadline = asyncio.get_running_loop().time() + timeout
        while True:
            try:
                if stat.S_ISSOCK(os.stat(self.upstream_path).st_mode):
                    reader, writer = await asyncio.open_unix_connection(self.upstream_path)
                    writer.close()
                    await writer.wait_closed()
                    return
            except (FileNotFoundError, ConnectionError, OSError):
                pass

            if asyncio.get_running_loop().time() >= deadline:
                raise RuntimeError(f"upstream Docker socket not ready: {self.upstream_path}")
            await asyncio.sleep(0.1)

    def _remove_stale_frontend(self) -> None:
        with contextlib.suppress(FileNotFoundError):
            os.unlink(self.frontend_path)

    def _mirror_frontend_permissions(self) -> None:
        upstream_stat = os.stat(self.upstream_path)
        os.chown(self.frontend_path, upstream_stat.st_uid, upstream_stat.st_gid)
        os.chmod(self.frontend_path, stat.S_IMODE(upstream_stat.st_mode))

    async def start(self) -> None:
        await self._wait_for_upstream()
        self._remove_stale_frontend()
        self._server = await asyncio.start_unix_server(
            self._handle_client,
            path=self.frontend_path,
            start_serving=False,
        )
        self._mirror_frontend_permissions()
        await self._server.start_serving()
        _LOGGER.info(
            "Docker socket proxy listening on %s -> %s",
            self.frontend_path,
            self.upstream_path,
        )

    async def close(self) -> None:
        if self._server is None:
            return
        self._server.close()
        await self._server.wait_closed()
        self._server = None
        self._remove_stale_frontend()

    async def serve_forever(self) -> None:
        await self.start()
        if self._server is None:
            raise RuntimeError("proxy server failed to start")
        async with self._server:
            await self._server.serve_forever()

    async def _handle_client(
        self,
        client_reader: asyncio.StreamReader,
        client_writer: asyncio.StreamWriter,
    ) -> None:
        upstream_reader: asyncio.StreamReader | None = None
        upstream_writer: asyncio.StreamWriter | None = None

        try:
            upstream_reader, upstream_writer = await asyncio.open_unix_connection(
                self.upstream_path
            )
            while True:
                request = await _read_request_head(client_reader)
                if is_create_request(request.path):
                    body, raw_body = await _read_request_body(request, client_reader)
                    rewritten_request = rewrite_http_request(request, body)
                    if rewritten_request == request.raw + body:
                        upstream_writer.write(request.raw + raw_body)
                    else:
                        upstream_writer.write(rewritten_request)
                    await upstream_writer.drain()
                else:
                    upstream_writer.write(request.raw)
                    await upstream_writer.drain()
                    await _forward_request_body(request, client_reader, upstream_writer)

                if _request_wants_tunnel(request):
                    await _bidirectional_relay(
                        client_reader,
                        client_writer,
                        upstream_reader,
                        upstream_writer,
                    )
                    return

                response = await _read_response(upstream_reader, request.method)
                if is_rewrite_target(request.path):
                    client_writer.write(rewrite_http_response(request.path, response))
                else:
                    client_writer.write(response.raw)
                await client_writer.drain()

                if not (
                    _uses_keep_alive(request.version, request.headers)
                    and _uses_keep_alive(response.version, response.headers)
                ):
                    return
        except asyncio.IncompleteReadError:
            _LOGGER.debug("connection closed while reading Docker API stream")
        except Exception:
            _LOGGER.exception("Docker proxy request failed")
        finally:
            if upstream_writer is not None:
                upstream_writer.close()
                with contextlib.suppress(Exception):
                    await upstream_writer.wait_closed()
            client_writer.close()
            with contextlib.suppress(Exception):
                await client_writer.wait_closed()
