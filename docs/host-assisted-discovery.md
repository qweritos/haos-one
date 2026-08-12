# Host-assisted discovery on Docker Desktop and Colima

Docker Desktop and Colima normally place containers behind a Linux VM. HAOS
One can reach the internet from that VM, but LAN multicast used by Home
Assistant's Zeroconf/mDNS and SSDP discovery does not cross the VM boundary.

`haos-one-net` provides an opt-in workaround:

1. A native host process joins the physical LAN's mDNS and SSDP groups.
2. A WireGuard tunnel becomes HAOS One's external IPv4 path after the
   authenticated relay heartbeat.
3. The host source-NATs guest egress, accepts Home Assistant HTTP on TCP 8123,
   and advertises its LAN address through mDNS, so the router needs no static
   route and Docker publishes no ports.

When the dummy NetworkManager compatibility service is enabled, it reports
`haoswg0` and the tunnel IPv4 configuration as Supervisor's primary interface
only while the HAOS One guest agent has activated that path. The reported
device deliberately remains fake Ethernet for Supervisor compatibility. If the
projection is absent or stale, the service returns to its ordinary `eth0`
compatibility view automatically.

The container initiates WireGuard toward `host.docker.internal`. Consequently,
Colima's default `shared` networking and SSH port forwarder work without UDP
port forwarding, bridged networking, `socket_vmnet`, or profile changes.

## Requirements and limitations

- Docker Desktop on macOS or Windows, or Colima with its Docker runtime on
  macOS.
- Administrator/root access on the host and the existing `--privileged`
  container mode.
- IPv4 mDNS and SSDP. IPv6, Matter/Thread routing, DHCP/ARP bridging, arbitrary
  multicast, and container-to-LAN service advertisements are not forwarded.
- The host agent runs in the foreground. Stop it with `Ctrl-C`; it removes its
  owned routes, PF/WinNAT state, and tunnel.
- External guest traffic intentionally depends on the helper after activation.
  An exact route to the WireGuard endpoint remains on the Docker/Colima path so
  the tunnel can recover without routing recursion.

On macOS the implementation uses an isolated `com.apple/haos-one` PF anchor.
PF is an implementation detail rather than a supported Apple product API, so
run `cleanup` after an unclean shutdown and after major macOS upgrades.

## Install the host CLI

Download the executable for the host platform and `SHA256SUMS` from the
project release, then verify the executable against that manifest:

- `haos-one-net-mac-intel` for Intel Macs.
- `haos-one-net-mac-apple-silicon` for Apple Silicon Macs.
- `haos-one-net-windows.exe` for 64-bit Windows.

The download is self-contained; no archive extraction, separate
`wireguard-go`, or installer is required. Rename or install it as
`haos-one-net` somewhere on `PATH`. On Windows the executable verifies and
loads its embedded, signed Wintun component from a private per-user cache when
the tunnel starts.

For development from this checkout:

```bash
go build -o ./bin/haos-one-net ./cmd/haos-one-net
```

## Generate configuration

Start Docker Desktop or Colima and run:

```bash
haos-one-net init --runtime auto
```

The host publishes `homeassistant.local` by default. Choose a different mDNS
name when initializing if the default conflicts with another device:

```bash
haos-one-net init --runtime auto --dns-name my-home.local
```

Runtime detection recognizes Docker Desktop and active Colima Docker contexts.
Use `--runtime colima` when generating configuration before starting Colima.
The command creates mode-0600 `host.yaml` and `guest.yaml` files and prints
ready-to-use Docker Run and Compose settings.
Existing configuration is not overwritten; use `--force` only when intentionally
rotating both WireGuard keys.

The default-route Ethernet/Wi-Fi adapter and its connected IPv4 subnet are used
automatically. `--lan-interface` selects a different adapter without changing
the host default route, and the adapter's connected IPv4 subnet is still
detected automatically:

```bash
haos-one-net init --runtime docker-desktop --lan-interface "Ethernet 2"
```

Use repeatable `--lan-cidr` values only to override or extend the automatically
detected connected subnet with routed LAN prefixes:

```bash
haos-one-net init --runtime colima \
  --lan-interface en0 \
  --lan-cidr 192.168.88.0/24 \
  --lan-cidr 10.20.0.0/16
```

`--host-endpoint` overrides `host.docker.internal`. This is useful when the LAN
itself uses Colima's internal `192.168.5.0/24` and a physical device occupies
the exact host-endpoint address.

`init` refuses a LAN prefix that overlaps an active Docker bridge. Move that
Docker network to a non-overlapping subnet before enabling tunneled LAN routes.

### Windows trusted LAN profile

On Windows, mark the physical home Ethernet or Wi-Fi interface selected with
`--lan-interface` as a **Private** network. Do this only for a LAN you trust;
do not change Docker Desktop's virtual or NAT adapters. This lets Windows apply
the expected local-discovery and management firewall policy without changing
the interface gateway, routes, or metrics.

In the Windows 11 GUI:

1. Open **Settings > Network & internet**.
2. Open the selected **Ethernet** or **Wi-Fi** connection.
3. Under **Network profile type**, select **Private network**.

Or use an elevated PowerShell, substituting the interface passed to
`--lan-interface`:

```powershell
Get-NetConnectionProfile
Set-NetConnectionProfile -InterfaceAlias "Ethernet 2" -NetworkCategory Private
```

## Run

Start the host agent first, using the absolute path printed by `init`:

```bash
sudo haos-one-net host run --config "/absolute/path/to/host.yaml"
```

Commands that load configuration use an explicit `--config` first, then
`HAOS_ONE_NET_CONFIG`, then the platform default. For example:

```bash
export HAOS_ONE_NET_CONFIG="/absolute/path/to/host.yaml"
sudo --preserve-env=HAOS_ONE_NET_CONFIG haos-one-net host run
```

On Windows, run the generated command from an Administrator PowerShell instead:

```powershell
.\haos-one-net.exe host run --config "$env:APPDATA\haos-one\net\host.yaml"
```

Then start HAOS One. There is no host networking, WireGuard UDP publication,
or Home Assistant port publication:

```bash
docker volume create haos-data
docker run --name haos -ti --privileged \
  -v "/absolute/path/to/guest.yaml:/etc/haos-one/desktop-network.yaml:ro" \
  -v haos-data:/mnt/data \
  qweritos/haos-one
```

Compose example:

```yaml
services:
  haos:
    image: qweritos/haos-one
    privileged: true
    volumes:
      - /absolute/path/to/guest.yaml:/etc/haos-one/desktop-network.yaml:ro
      - haos-data:/mnt/data

volumes:
  haos-data:
```

For Colima, run these commands against its normal Docker context. Do not enable
`network.address`, bridged networking, or the gRPC port forwarder.

Open `http://homeassistant.local:8123` (or the configured `--dns-name`) from a
LAN client. The host companion advertises the physical host IPv4 address and
forwards TCP 8123 through WireGuard to the guest tunnel address.

## Diagnose and clean up

```bash
sudo haos-one-net doctor --config "/absolute/path/to/host.yaml" --container haos
sudo haos-one-net cleanup --config "/absolute/path/to/host.yaml"
```

Use the same commands without `sudo` from Administrator PowerShell on Windows.

The guest installs split-default IPv4 routes only after authenticated relay
heartbeats arrive. If the host agent disappears, those routes remain so traffic
does not silently bypass the configured external gateway; the endpoint escape
route allows reconnection. `cleanup` reads the managed state file and does not
remove unrelated PF, firewall, route, or WinNAT state.
