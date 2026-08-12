# HAOS One guest agent

`haos-one-agent` is the single, always-running Linux daemon embedded in the
HAOS One image. It replaces the former nested Python compatibility container
and also activates host-assisted networking when a guest configuration is
mounted.

The daemon has no operational subcommands. Systemd starts it before Supervisor:

```bash
systemctl status haos-one-agent.service --no-pager
journalctl -u haos-one-agent.service -n 100 --no-pager
haos-one-agent --version
```

It owns these independent components:

- the Docker API compatibility proxy at `/run/docker.sock`, forwarding to
  `/run/docker-real.sock`;
- the fake-Ethernet NetworkManager D-Bus projection used by Supervisor;
- idempotent Supervisor udev-shim migration;
- the WireGuard tunnel, routes, HTTP ingress, mDNS, and SSDP guest relay when
  `/etc/haos-one/desktop-network.yaml` exists.

Networking is detected from that read-only mount; no enablement environment
variable is required. Removing or replacing the configuration causes the
networking component to withdraw or restart without stopping the compatibility
components. The YAML schema and `HAOS_ONE_NET_CONFIG` host-side override remain
compatible with the preview release.

The only retained Python file is
`/opt/haos-one-agent/udev-shim/sitecustomize.py`. It is injected into the
Supervisor process because that compatibility hook must execute in
Supervisor's Python interpreter; there is no Python agent or helper container.

For an existing installation, starting the new image preserves
`/mnt/data/supervisor` and the generated WireGuard keys. In `auto` mode the
agent recreates `hassio_supervisor` once only when the stored container
configuration does not yet contain the udev-shim mount.

Useful checks:

```bash
test -S /run/docker.sock
docker -H unix:///run/docker.sock info
busctl get-property org.freedesktop.NetworkManager \
  /org/freedesktop/NetworkManager org.freedesktop.NetworkManager PrimaryConnection
cat /run/haos-one-agent/version.json
```

The version file is consumed by `haos-one-host doctor` to detect an incompatible
host/guest relay protocol before reporting the networking path healthy.
