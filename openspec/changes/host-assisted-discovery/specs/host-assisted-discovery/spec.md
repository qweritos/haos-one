## ADDED Requirements

### Requirement: Opt-in desktop LAN access
As a HAOS One user, I want an opt-in host-assisted network path from a VM-backed Docker runtime to my physical LAN, so that Home Assistant can discover and connect to local devices without host networking or router changes.

HAOS One SHALL provide mount-activated IPv4 LAN discovery and reachability for supported VM-backed Docker runtimes without requiring host networking or router changes.

#### Scenario: Acceptance criteria
- **Given** HAOS One runs in Docker Desktop on macOS or Windows, or in Colima's default Docker runtime on macOS
- **When** the user mounts the generated guest configuration read-only at `/etc/haos-one/desktop-network.yaml` and retains privileged mode with `/dev/net/tun`
- **Then** Home Assistant receives IPv4 mDNS and SSDP discovery and can initiate connections to selected LAN prefixes without `--network host` or a published WireGuard UDP port
- **Given** a valid guest configuration exists at `/etc/haos-one/desktop-network.yaml`
- **When** HAOS One starts
- **Then** it enables the guest networking agent automatically without requiring an environment variable
- **Given** Colima uses its default `shared` network with `address: false` and the TCP-only SSH forwarder
- **When** host-assisted networking is enabled
- **Then** it works without editing the Colima profile, enabling `socket_vmnet`, bridged networking, a VM address, or the experimental gRPC forwarder
- **Given** the guest configuration file is absent
- **When** HAOS One starts
- **Then** existing Docker or Colima networking behavior remains unchanged

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
- **Given** the host is multi-homed or reaches routed LANs
- **When** repeated `--lan-interface` and `--lan-cidr` values are supplied
- **Then** every selected interface and prefix is represented without replacing unrelated host routes
- **Given** configuration files already exist
- **When** `init` is run without `--force`
- **Then** it refuses to overwrite the configuration or rotate its keys
- **Given** `host.docker.internal` conflicts with a physical device or cannot be used
- **When** `--host-endpoint` is supplied
- **Then** the guest uses that explicit IPv4 endpoint while preserving all other generated settings
- **Given** the generated launch settings advertise `http://localhost:8123`
- **When** the current HAOS release serves Home Assistant on a different container-side listener or uses a canonical redirect listener
- **Then** the generated Run and Compose port mapping reaches the onboarding or dashboard page without redirecting the browser to an unexposed host port

### Requirement: Authenticated tunnel lifecycle and safe fallback
As a HAOS One user, I want LAN routes to exist only while the host companion is authenticated and healthy, so that failures fall back to ordinary container networking instead of black-holing traffic.

The guest agent SHALL activate selected LAN routes only while the authenticated tunnel and relay heartbeat are healthy and SHALL withdraw them on host loss.

#### Scenario: Acceptance criteria
- **Given** the guest starts behind a Docker Desktop or Colima VM
- **When** it resolves the configured host endpoint
- **Then** it pins that endpoint through the original VM default route and initiates the encrypted WireGuard tunnel from the container
- **Given** kernel WireGuard support is available in the guest
- **When** the guest creates its interface
- **Then** it prefers the kernel implementation and otherwise starts the bundled `wireguard-go` helper
- **Given** a valid WireGuard handshake and relay heartbeat are present
- **When** the guest activates selected LAN prefixes
- **Then** WireGuard `AllowedIPs` and operating-system routes change atomically
- **Given** the host companion or relay heartbeat disappears
- **When** 15 seconds elapse without recovery
- **Then** the guest withdraws the selected LAN routes and ordinary Docker or Colima NAT becomes the fallback
- **Given** the host endpoint, default adapter, or adapter address changes
- **When** the companion observes the change
- **Then** it refreshes endpoint pinning, relay membership, routes, and NAT without regenerating keys

### Requirement: Host-side forwarding and source NAT
As a LAN operator, I want device connections to appear from the Docker host, so that LAN devices can reply without static routes or router configuration.

The host companion SHALL forward and source-NAT selected guest LAN traffic while isolating all state it owns from unrelated host networking.

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

The relay SHALL forward inbound mDNS discovery with the source and interface semantics required by Home Assistant while suppressing container advertisements toward the LAN.

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
- **Then** it covers mDNS query, response, and announcement handling; SSDP search, reply, and notify handling; fragmentation; duplicate suppression; source preservation; route failover; and SNAT
- **Given** synthetic mDNS and SSDP LAN devices and a black-box Home Assistant instance
- **When** discovery and connection tests run
- **Then** devices appear in Home Assistant, advertised addresses are reachable, the fixture observes the host LAN address as the SNAT source, and container-originated advertisements do not reach the LAN
- **Given** supported runtime smoke environments
- **When** release validation runs
- **Then** it covers Docker Desktop on macOS amd64 and arm64, Docker Desktop on Windows amd64 including pre-existing WinNAT, and Colima 0.10.3 with default shared networking and no profile changes
- **Given** version 1 is enabled
- **When** unsupported traffic is encountered
- **Then** IPv6, Matter or Thread reachability, DHCP or ARP bridging, arbitrary multicast, inbound HA service advertising, Docker Desktop Kubernetes, and Colima containerd remain explicitly out of scope
