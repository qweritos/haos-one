# `haos_one_compat`

`haos_one_compat` is a small helper container started inside the HAOS guest.
It exists to provide compatibility shims that make Home Assistant Supervisor
behave like it is running on a normal HAOS host, without changing Supervisor
itself.

Today it has two jobs:

- a D-Bus shim for NetworkManager
- a Docker socket interceptor

## Lifecycle

`haos-one-compat.service` builds and runs the container early in boot.

Important pieces:

- host service: `/etc/systemd/system/haos-one-compat.service`
- socket override: `/etc/systemd/system/docker.socket.d/override.conf`
- process entrypoint: `/opt/haos-one-compat/haos_one_compat/__main__.py`

The service mounts:

- `/run/dbus` into the container so the dummy NetworkManager can talk on the host bus
- `/run` into the container as `/host-run` so the Docker proxy can own the guest-facing socket

The compat service must be up before Supervisor starts using Docker. The systemd unit
waits until `/run/docker.sock` exists before continuing.

## D-Bus Shim

The D-Bus part emulates only the NetworkManager surface Supervisor actually uses.
It is intentionally narrow and state-light.

Code:

- `/opt/haos-one-compat/haos_one_compat/dummy_nm.py`

Behavior:

- exports a fake `org.freedesktop.NetworkManager`
- implements only the methods/properties Supervisor queries
- returns stable, synthetic data instead of managing real host networking

This is enabled by `USE_DUMMY_NETWORKMANAGER=1`, which is the current default.
If disabled, the compat container still runs for Docker interception, but the dummy
NetworkManager task is skipped.

### D-Bus Validation

Inside the running `haos` container:

```bash
systemctl is-active haos-one-compat.service
journalctl -u haos-one-compat.service -n 50 --no-pager
```

Expected log line:

```text
Dummy NetworkManager service running on system bus
```

If Supervisor still reports NetworkManager issues, inspect:

- `journalctl -u haos-one-compat.service`
- `journalctl -u hassos-supervisor.service`

## Docker Interceptor

The Docker daemon does not listen on `/run/docker.sock` directly anymore.
Instead:

- the real dockerd socket is moved to `/run/docker-real.sock`
- `haos_one_compat` binds `/run/docker.sock`
- the compat proxy forwards requests to `/run/docker-real.sock`

This is done by overriding `docker.socket`:

```ini
[Socket]
ListenStream=
ListenStream=/run/docker-real.sock
```

The proxy code is here:

- `/opt/haos-one-compat/haos_one_compat/docker_proxy.py`
- `/opt/haos-one-compat/haos_one_compat/docker_rules.py`

### Why it exists

Supervisor marks the system unsupported when it sees extra software outside the
expected Home Assistant container set. In this project, the extra container is
`haos_one_compat` itself.

Supervisor also includes `Domainname` and `HostConfig.Ulimits` in container-create
requests. Docker cannot apply those settings inside an unprivileged nested LXC and
fails during container start with a `/proc/sys/kernel/domainname` permission error.

The relevant Supervisor logic is in `../supervisor/supervisor/resolution/evaluations/container.py`:

- it lists Docker containers
- it resolves image metadata
- unknown images become `UnsupportedReason.SOFTWARE`

Instead of patching Supervisor, the proxy filters the compat container out of the
Docker API response Supervisor consumes and removes the two unsupported create
options before dockerd receives the request.

### What is intercepted

The current scope is intentionally small:

- `/containers/json`
  - hides `haos_one_compat`
- `/info`
  - adds `Warnings: ["HAOS compat: intercepted"]`
- `/containers/create` (including versioned API paths)
  - removes top-level `Domainname`
  - removes `HostConfig.Ulimits`
  - injects the Supervisor udev shim when enabled

Everything else is passed through unchanged.

Notably:

- `/version` is not modified
- container inspect payloads are not rewritten
- network APIs are not rewritten
- other request bodies are not modified

### Supervisor udev monitor

An unprivileged outer LXC cannot provide the nested Supervisor container access
to the kernel udev event monitor. Without compatibility handling, Supervisor adds
the `privileged` unhealthy reason and blocks guarded operations such as app
installation.

For the `hassio_supervisor` create request only, the proxy can mount an isolated
Python startup shim and prepend it to `PYTHONPATH`. The shim replaces the failing
kernel event monitor with an idle pollable monitor. Static hardware enumeration
continues to work, but live hardware hotplug events are unavailable.

Set `USE_UDEV_SHIM` on the outer `haos` container:

- `auto` (default) enables the shim when root is remapped through a user namespace
- `force` always enables it
- `off` disables it

The shim does not edit Supervisor source or persist files inside its image.

### Keep-alive caveat and fix

The main runtime bug during development was that `curl` looked correct, but
`docker ps` still showed `haos_one_compat`.

Root cause:

- the original proxy made a one-time decision per connection
- Docker CLI sends `HEAD /_ping` first
- then it sends `GET /containers/json` on the same keep-alive socket
- after the first pass-through decision, the later list call was no longer rewritten

The proxy now inspects each HTTP request on persistent connections and only switches
to raw bidirectional relay for streaming or hijacked endpoints such as logs, stats,
attach, and exec streams.

## Docker Validation

Inside the running `haos` container:

```bash
systemctl is-active haos-one-compat.service
ls -l /run/docker.sock /run/docker-real.sock
docker -H unix:///run/docker.sock ps
docker -H unix:///run/docker-real.sock ps
docker -H unix:///run/docker.sock info
```

Expected:

- `/run/docker.sock` exists and is owned by the proxy
- `/run/docker-real.sock` exists and is owned by dockerd
- `docker -H unix:///run/docker.sock ps` does not show `haos_one_compat`
- `docker -H unix:///run/docker-real.sock ps` does show `haos_one_compat`
- `docker -H unix:///run/docker.sock info` shows warning `HAOS compat: intercepted`

Raw API checks:

```bash
curl --unix-socket /run/docker.sock -s http://localhost/containers/json?all=1 | jq .
curl --unix-socket /run/docker-real.sock -s http://localhost/containers/json?all=1 | jq .
curl --unix-socket /run/docker.sock -s http://localhost/info | jq .
```

Expected:

- proxied `/containers/json` omits `/haos_one_compat`
- real `/containers/json` includes `/haos_one_compat`
- proxied `/info` contains `Warnings` entry `HAOS compat: intercepted`

## Debugging

### Service state

```bash
systemctl status haos-one-compat.service --no-pager
systemctl status docker.socket docker.service --no-pager
systemctl status hassos-supervisor.service --no-pager
```

### Logs

```bash
journalctl -u haos-one-compat.service -n 100 --no-pager
journalctl -u hassos-supervisor.service -n 100 --no-pager
```

Useful things to look for:

- `Docker socket proxy listening on /host-run/docker.sock -> /host-run/docker-real.sock`
- `Dummy NetworkManager service running on system bus`
- Docker daemon connection errors from Supervisor
- `start-limit-hit` on `hassos-supervisor.service` after a temporary socket outage

If Supervisor hit start limits after a failed boot sequence:

```bash
systemctl reset-failed hassos-supervisor.service
systemctl start hassos-supervisor.service
```

### Socket sanity

```bash
ss -xlpn | grep docker
```

Expected:

- dockerd listens on `/run/docker-real.sock`
- the compat proxy owns `/run/docker.sock`

### Common failure modes

`docker ps` still shows `haos_one_compat`

- verify you are using `/run/docker.sock`, not `/run/docker-real.sock`
- verify `haos-one-compat.service` restarted after code changes
- compare raw API output from both sockets
- if `curl` is correct but Docker CLI is wrong, suspect keep-alive handling again

Supervisor cannot connect to Docker

- ensure `haos-one-compat.service` is active
- ensure `/run/docker.sock` exists
- ensure `docker.socket` override still points dockerd to `/run/docker-real.sock`
- reset Supervisor start limits if needed

Unsupported software warning still appears

- confirm proxied `/containers/json` does not contain `/haos_one_compat`
- inspect Supervisor logs after restart
- if needed, inspect `../supervisor/supervisor/resolution/evaluations/container.py`

## Tests

Local tests for the compat container are here:

- `/opt/haos-one-compat/tests/test_docker_rules.py`
- `/opt/haos-one-compat/tests/test_docker_proxy.py`

Useful commands:

```bash
python3 -m unittest discover -s /opt/haos-one-compat/tests -p 'test_*.py'
python3 -m py_compile /opt/haos-one-compat/haos_one_compat/docker_proxy.py \
  /opt/haos-one-compat/haos_one_compat/docker_rules.py
```

For runtime validation, prefer a real `haos-one` container over synthetic local-only
checks, because the socket ordering and systemd behavior matter here.
