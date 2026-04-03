"""Module entrypoint for compat services."""

from __future__ import annotations

import asyncio
import contextlib
import logging
import os

from .docker_proxy import DockerSocketProxy
from .dummy_nm import build_arg_parser as build_dummy_nm_arg_parser
from .dummy_nm import run_service as run_dummy_nm_service

_LOGGER = logging.getLogger(__name__)


def _env_enabled(name: str, default: bool) -> bool:
    value = os.getenv(name)
    if value is None:
        return default
    return value.lower() in {"1", "true", "yes", "on"}


def build_arg_parser():
    parser = build_dummy_nm_arg_parser()
    parser.description = "haos_one_compat services"
    parser.add_argument(
        "--frontend-socket",
        default="/host-run/docker.sock",
        help="Frontend Docker API UNIX socket path",
    )
    parser.add_argument(
        "--upstream-socket",
        default="/host-run/docker-real.sock",
        help="Upstream Docker API UNIX socket path",
    )
    return parser


async def run_services(args) -> None:
    proxy = DockerSocketProxy(
        frontend_path=args.frontend_socket,
        upstream_path=args.upstream_socket,
    )
    tasks = {
        asyncio.create_task(proxy.serve_forever(), name="docker-proxy"),
    }

    if _env_enabled("USE_DUMMY_NETWORKMANAGER", True):
        tasks.add(asyncio.create_task(run_dummy_nm_service(args), name="dummy-nm"))
    else:
        _LOGGER.info("Dummy NetworkManager disabled")

    done, pending = await asyncio.wait(tasks, return_when=asyncio.FIRST_COMPLETED)

    first_error: BaseException | None = None
    for task in done:
        try:
            task.result()
        except Exception as exc:  # pragma: no cover - exercised by integration behavior
            first_error = exc
            break
        first_error = RuntimeError(f"{task.get_name()} exited unexpectedly")
        break

    for task in pending:
        task.cancel()
    for task in pending:
        with contextlib.suppress(asyncio.CancelledError):
            await task
    await proxy.close()

    if first_error is not None:
        raise first_error


def main() -> None:
    parser = build_arg_parser()
    args = parser.parse_args()

    logging.basicConfig(
        level=logging.DEBUG if args.verbose else logging.INFO,
        format="%(asctime)s %(levelname)s %(name)s: %(message)s",
    )

    asyncio.run(run_services(args))


if __name__ == "__main__":
    main()
