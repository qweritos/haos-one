## ADDED Requirements

### Requirement: Opt-in desktop LAN access
As a HAOS One user, I want an opt-in host-assisted network path from a VM-backed Docker runtime to my physical LAN, so that Home Assistant can discover and connect to local devices without host networking or router changes.

HAOS One SHALL provide mount-activated IPv4 LAN discovery and reachability for supported VM-backed Docker runtimes without requiring host networking or router changes.

#### Scenario: Acceptance criteria
- **Given** HAOS One runs in Docker Desktop on macOS or Windows, or in Colima's default Docker runtime on macOS
- **When** the user mounts the generated guest configuration read-only at `/etc/haos-one/desktop-network.yaml` and retains privileged mode with `/dev/net/tun`
- **Then** Home Assistant receives IPv4 mDNS and SSDP discovery and uses the authenticated WireGuard path for external IPv4 access without `--network host`, Docker port publication, or a published WireGuard UDP port
- **Given** a valid guest configuration exists at `/etc/haos-one/desktop-network.yaml`
- **When** HAOS One starts
- **Then** it enables the guest networking agent automatically without requiring an environment variable
- **Given** Colima uses its default `shared` network with `address: false` and the TCP-only SSH forwarder
- **When** host-assisted networking is enabled
- **Then** it works without editing the Colima profile, enabling `socket_vmnet`, bridged networking, a VM address, or the experimental gRPC forwarder
- **Given** the guest configuration file is absent
- **When** HAOS One starts
- **Then** existing Docker or Colima networking behavior remains unchanged
- **Given** host-assisted networking is active
- **When** a LAN client opens the advertised Home Assistant HTTP endpoint
- **Then** the host companion accepts the connection on the selected host LAN address and forwards it through WireGuard to Home Assistant without a Docker-published port

### Requirement: Deterministic configuration bootstrap
As an operator, I want one cross-platform CLI to create a conflict-free host and guest configuration, so that setup is repeatable and does not require manual WireGuard design.

`haos-one-net init` SHALL generate secure, conflict-free, runtime-appropriate configuration and launch instructions without requiring manual WireGuard design.

#### Scenario: Acceptance criteria
- **Given** Docker Desktop or Colima is available
- **When** the operator runs `haos-one-net init --runtime auto|docker-desktop|colima`
- **Then** the CLI detects or honors the runtime, generates mode-restricted WireGuard keys and host/guest YAML, and prints ready-to-use Docker Run and Compose settings that activate the feature through the read-only configuration mount alone
- **Given** no tunnel CIDR is supplied
- **When** initialization selects a network
- **Then** it prefers `10.203.0.0/30`, assigns host `.1` and guest `.2`, uses MTU 1280 and host UDP port 51821, and rejects overlap with selected LAN prefixes or active Docker bridges
- **Given** the operator supplies `--lan-interface` without `--lan-cidr`
- **When** initialization inspects that interface
- **Then** it derives the connected IPv4 LAN prefix automatically without changing the host default route or route metrics
- **Given** the host is multi-homed or reaches routed LANs
- **When** repeated `--lan-interface` and `--lan-cidr` values are supplied
- **Then** every selected interface is represented and the explicit prefixes override or extend automatic detection without replacing unrelated host routes
- **Given** configuration files already exist
- **When** `init` is run without `--force`
- **Then** it refuses to overwrite the configuration or rotate its keys
- **Given** `host.docker.internal` conflicts with a physical device or cannot be used
- **When** `--host-endpoint` is supplied
- **Then** the guest uses that explicit IPv4 endpoint while preserving all other generated settings
- **Given** no DNS name is supplied
- **When** initialization generates host configuration
- **Then** the host advertises `homeassistant.local` and `--dns-name` can select another valid `.local` name
- **Given** a command accepts `--config`
- **When** the flag is omitted and `HAOS_ONE_NET_CONFIG` is set
- **Then** the command resolves its configuration through the shared environment-aware path utility before using the platform default
- **Given** launch settings are generated
- **When** host-assisted networking is enabled
- **Then** the Run and Compose snippets omit host networking and all Docker port publication because the host companion owns HTTP ingress

### Requirement: Authenticated tunnel lifecycle and external default path
As a HAOS One user, I want the authenticated host companion to be HAOS One's external IPv4 gateway, so that Internet and LAN traffic consistently use the host-assisted path.

The guest agent SHALL activate default IPv4 routing through WireGuard after authenticating the host and SHALL retain a narrowly scoped Docker or Colima escape route for WireGuard bootstrap and recovery.

#### Scenario: Acceptance criteria
- **Given** the guest starts behind a Docker Desktop or Colima VM
- **When** it resolves the configured host endpoint
- **Then** it pins that endpoint through the original VM default route and initiates the encrypted WireGuard tunnel from the container
- **Given** kernel WireGuard support is available in the guest
- **When** the guest creates its interface
- **Then** it prefers the kernel implementation and otherwise starts the bundled `wireguard-go` helper
- **Given** a valid WireGuard handshake and relay heartbeat are present
- **When** the guest activates external routing
- **Then** WireGuard `AllowedIPs` and split-default operating-system routes change atomically while the exact host endpoint remains pinned through the original VM route
- **Given** the host companion or relay heartbeat disappears
- **When** 15 seconds elapse without recovery
- **Then** external traffic remains bound to WireGuard and the endpoint escape route permits the tunnel to reconnect without silently falling back to ordinary Docker or Colima NAT
- **Given** the host endpoint, default adapter, or adapter address changes
- **When** the companion observes the change
- **Then** it refreshes endpoint pinning, relay membership, routes, and NAT without regenerating keys
- **Given** the dummy NetworkManager compatibility interceptor is enabled
- **When** the HAOS One-owned `haoswg0` exists and carries the authenticated split-default external route
- **Then** the interceptor reports `haoswg0` and its tunnel IPv4 configuration as Supervisor's primary connection while retaining a fake Ethernet device type, and falls back to the ordinary `eth0` compatibility view when that active projection is absent or stale

### Requirement: Host-side forwarding and source NAT
As a LAN operator, I want device connections to appear from the Docker host, so that LAN devices can reply without static routes or router configuration.

The host companion SHALL forward and source-NAT guest external IPv4 traffic, accept Home Assistant HTTP ingress on the host address, and isolate all state it owns from unrelated host networking.

#### Scenario: Acceptance criteria
- **Given** the host is macOS
- **When** `haos-one-net host run` starts with administrator privileges
- **Then** it creates a `wireguard-go` utun, enables IPv4 forwarding, and installs forwarding and SNAT rules only in the isolated `com.apple/haos-one` PF anchor
- **Given** the host is Windows
- **When** `haos-one-net host run` starts from an Administrator session
- **Then** it uses bundled WireGuard/Wintun support, enables interface forwarding, adds Private-profile firewall rules, and configures WinNAT for the selected tunnel prefix
- **Given** Windows already has a compatible NAT prefix
- **When** the tunnel is allocated
- **Then** the companion reuses compatible state or selects a free `/30`, and fails without modifying unrelated networks when existing NAT state is incompatible
- **Given** Home Assistant connects to a discovered LAN address through the tunnel
- **When** the connection reaches the LAN fixture
- **Then** the observed source is the selected host LAN address and no router change is required
- **Given** a LAN client connects to the selected host address on TCP port 8123
- **When** host-assisted networking is active
- **Then** the companion forwards the stream to the guest tunnel address on TCP port 8123 while preserving exclusive ownership and reporting an occupied host port
- **Given** the companion stops normally or performs stale-state recovery
- **When** host networking is removed
- **Then** only recorded HAOS One interfaces, forwarding changes, routes, firewall rules, PF state, and NAT state are restored or deleted

### Requirement: Versioned discovery relay
As a maintainer, I want discovery messages carried by a bounded protocol inside WireGuard, so that multicast forwarding is secure, diagnosable, and resilient to duplicate or oversized packets.

The discovery relay SHALL use a versioned, bounded, correlated protocol inside the authenticated WireGuard tunnel.

#### Scenario: Acceptance criteria
- **Given** host and guest companions have an authenticated tunnel
- **When** discovery traffic is exchanged on UDP port 47821 inside WireGuard
- **Then** messages include a protocol version, interface identity, message identity, and request correlation data
- **Given** a payload exceeds one relay datagram
- **When** it is transported
- **Then** it is fragmented and reassembled within configured bounds, and invalid or incomplete fragment sets are discarded
- **Given** the same discovery message arrives repeatedly from multiple paths or interfaces
- **When** duplicate suppression evaluates it
- **Then** it is delivered at most once within the suppression window without suppressing distinct correlated replies

### Requirement: mDNS and Zeroconf forwarding
As a Home Assistant user, I want normal Zeroconf discovery to cross the desktop VM boundary, so that integrations appear as if Home Assistant were directly attached to the LAN.

The relay SHALL forward inbound mDNS discovery with the source and interface semantics required by Home Assistant, publish a controlled host-owned Home Assistant identity, and suppress arbitrary container advertisements toward the LAN.

#### Scenario: Acceptance criteria
- **Given** Home Assistant sends an mDNS query
- **When** the guest relay classifies it
- **Then** the host forwards it to `224.0.0.251:5353` on every selected LAN interface
- **Given** a LAN device sends an mDNS response or unsolicited announcement
- **When** the host receives it
- **Then** the guest injects it into the existing outer-container network namespace with the original source address, source port, interface identity, and applicable TTL semantics
- **Given** nested Home Assistant Core uses host networking inside HAOS One
- **When** a relayed response is injected
- **Then** Home Assistant receives it on its normal `eth0` path without requiring the WireGuard adapter to be enabled in Home Assistant
- **Given** Home Assistant or an add-on emits an mDNS advertisement
- **When** the relay evaluates outbound traffic
- **Then** version 1 does not forward that advertisement onto the physical LAN
- **Given** the host companion is active on a selected LAN interface
- **When** it starts, receives a matching query, refreshes its lease, changes address, or stops
- **Then** it publishes or withdraws A, PTR, SRV, and TXT records for the configured `.local` name using the selected host LAN IPv4 address and Home Assistant TCP port 8123

### Requirement: SSDP forwarding
As a Home Assistant user, I want SSDP searches and device notifications to cross the desktop VM boundary, so that UPnP integrations can be discovered normally.

The relay SHALL forward correlated SSDP searches, replies, and inbound notifications while suppressing container advertisements toward the LAN.

#### Scenario: Acceptance criteria
- **Given** Home Assistant sends an SSDP `M-SEARCH`
- **When** the guest relay classifies it
- **Then** the host forwards it to `239.255.255.250:1900` and the selected interface's IPv4 broadcast destination
- **Given** LAN devices return unicast SSDP search replies
- **When** replies arrive within the advertised `MX` interval plus two seconds
- **Then** the host correlates them to the originating search and relays them to the guest while preserving their source identity
- **Given** a LAN device sends SSDP `NOTIFY`
- **When** the host receives it on a selected interface
- **Then** the notification is relayed inward to Home Assistant
- **Given** Home Assistant or an add-on emits an SSDP advertisement or notification
- **When** the relay evaluates outbound traffic
- **Then** version 1 does not forward it onto the physical LAN

### Requirement: Diagnostics and idempotent cleanup
As an operator, I want actionable health checks and narrowly scoped cleanup, so that I can diagnose failures and recover safely without disturbing unrelated networking.

The CLI SHALL diagnose each network layer and remove only recorded HAOS One state through idempotent cleanup.

#### Scenario: Acceptance criteria
- **Given** a configured instance and container name
- **When** the operator runs `haos-one-net doctor --container <name>`
- **Then** the report checks runtime detection, endpoint resolution, WireGuard handshake, relay counters and heartbeat, selected routes, firewall or NAT state, and test discovery
- **Given** the companion was terminated, the VM restarted, an endpoint address changed, a port is occupied, or recorded state is stale
- **When** `doctor` or the next host run evaluates the instance
- **Then** it reports the failed layer and does not silently modify unrelated host networking
- **Given** `doctor` loads host configuration
- **When** it reports ingress and discovery state
- **Then** it includes the configured DNS name, TCP listener availability, and default-route mode
- **Given** generated configuration and managed runtime state exist
- **When** `cleanup` runs repeatedly
- **Then** it removes only recorded HAOS One state, succeeds idempotently, and keeps configuration unless `--purge` is supplied

### Requirement: Cross-platform packaging and regression coverage
As a maintainer, I want all guest and host artifacts built and tested for their supported platforms, so that releases do not regress discovery, fallback, or host networking safety.

Releases SHALL contain the supported guest and self-contained host artifacts and SHALL be validated against discovery, routing, failure, and cleanup acceptance criteria.

#### Scenario: Acceptance criteria
- **Given** amd64 and arm64 HAOS One images are built
- **When** image contents are inspected
- **Then** each image contains the Linux guest agent, its systemd unit, and a bundled `wireguard-go` fallback
- **Given** a project release is published
- **When** host downloads are inspected
- **Then** standalone `haos-one-net-mac-intel`, `haos-one-net-mac-apple-silicon`, and `haos-one-net-windows.exe` executables plus one `SHA256SUMS` file covering all three executables exist
- **Given** a host executable is downloaded
- **When** it creates its userspace WireGuard tunnel
- **Then** it runs the WireGuard engine in-process without requiring a separate `wireguard-go` executable, archive extraction, installer, or runtime dependency
- **Given** Linux namespace integration fixtures
- **When** the networking suite runs
- **Then** it covers mDNS query, response, controlled host announcement, and goodbye handling; SSDP search, reply, and notify handling; fragmentation; duplicate suppression; source preservation; default routing; endpoint escape routing; and SNAT
- **Given** synthetic mDNS and SSDP LAN devices and a black-box Home Assistant instance
- **When** discovery and connection tests run
- **Then** devices appear in Home Assistant, `homeassistant.local` or its configured replacement resolves to the host LAN address, TCP 8123 reaches Home Assistant without Docker port publication, the fixture observes the host LAN address as the SNAT source, and arbitrary container-originated advertisements do not reach the LAN
- **Given** supported runtime smoke environments
- **When** release validation runs
- **Then** it covers Docker Desktop on macOS amd64 and arm64, Docker Desktop on Windows amd64 including pre-existing WinNAT, and Colima 0.10.3 with default shared networking and no profile changes
- **Given** version 1 is enabled
- **When** unsupported traffic is encountered
- **Then** IPv6, Matter or Thread reachability, DHCP or ARP bridging, arbitrary multicast, arbitrary container service advertising, Docker Desktop Kubernetes, and Colima containerd remain explicitly out of scope
