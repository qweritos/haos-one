## 1. CLI and Configuration Contract

- [x] 1.1 Implement `haos-one-net init`, `host run`, `doctor`, and `cleanup` with macOS and Windows command-line parity.
- [x] 1.2 Generate protected host/guest WireGuard configuration, choose a non-overlapping `/30`, and preserve keys unless `--force` is explicit.
- [x] 1.3 Detect Docker Desktop and Colima, default to the physical route interface, and support repeated LAN interface/CIDR overrides plus `--host-endpoint`.
- [ ] 1.4 Generate Docker Run and Compose snippets that activate networking from the read-only guest configuration mount alone and whose advertised port 8123 reaches the active Home Assistant listener without a port-stripping redirect.

## 2. Guest Tunnel Lifecycle

- [ ] 2.1 Embed the Linux guest agent and systemd unit in amd64 and arm64 images, automatically starting it when `/etc/haos-one/desktop-network.yaml` exists and leaving normal networking unchanged when it does not.
- [x] 2.2 Prefer kernel WireGuard and bundle a `wireguard-go` fallback in the guest image, resolve the host endpoint, and pin it through the original VM route.
- [x] 2.3 Activate `AllowedIPs` and LAN routes only after a valid handshake and heartbeat, then withdraw them after 15 seconds of host loss.
- [x] 2.4 Refresh endpoint pinning and route state after VM, endpoint, default-route, or adapter address changes without rotating keys.

## 3. Host Forwarding and NAT

- [x] 3.1 Implement macOS utun creation, IPv4 forwarding, and isolated `com.apple/haos-one` PF forwarding/SNAT state.
- [x] 3.2 Implement Windows WireGuard/Wintun setup, interface forwarding, Private-profile firewall rules, and compatible WinNAT reuse or `/30` allocation.
- [x] 3.3 Record all owned interfaces, routes, forwarding changes, firewall/PF rules, and NAT state for safe stale-state recovery and idempotent cleanup.
- [x] 3.4 Monitor selected adapter membership and addresses, refreshing forwarding and NAT without disturbing unrelated host networks.

## 4. Discovery Relay

- [x] 4.1 Implement the versioned UDP 47821 relay protocol with interface identity, request correlation, bounded fragmentation/reassembly, and duplicate suppression.
- [x] 4.2 Forward mDNS queries to selected LAN interfaces and inject responses and unsolicited announcements with preserved source identity and TTL semantics.
- [x] 4.3 Forward SSDP `M-SEARCH` to multicast and broadcast, correlate replies for `MX + 2s`, and relay LAN `NOTIFY` messages inward.
- [x] 4.4 Inject discovery into the outer-container namespace used by nested Home Assistant Core while preventing container-originated advertisements from reaching the LAN.

## 5. Diagnostics, Documentation, and Release Artifacts

- [ ] 5.1 Make `doctor` report runtime, endpoint, handshake, heartbeat/counters, routes, firewall/NAT state, and a test discovery result.
- [x] 5.2 Make `cleanup` remove only recorded state, retain configuration by default, support `--purge`, and tolerate repeated or stale cleanup.
- [ ] 5.3 Document opt-in setup, supported runtimes, administrator requirements, limitations, recovery, and default Colima compatibility.
- [ ] 5.4 Publish self-contained host executables named `haos-one-net-mac-intel`, `haos-one-net-mac-apple-silicon`, and `haos-one-net-windows.exe`, with one `SHA256SUMS` file covering all three executables, alongside the container images; run userspace WireGuard in-process with no companion executable or archive extraction.

## 6. Automated Verification

- [x] 6.1 Add protocol unit tests for configuration validation, overlap handling, fragmentation, duplicate suppression, and discovery classification.
- [ ] 6.2 Add Linux namespace integration tests for mDNS, SSDP, source preservation, route failover, and host-side SNAT.
- [ ] 6.3 Add a black-box Home Assistant test with synthetic mDNS/SSDP devices, advertised-address connectivity, SNAT-source verification, and outbound-advertisement suppression.
- [ ] 6.4 Add failure tests for host termination, VM restart, endpoint changes, overlapping prefixes, multiple adapters, occupied ports, stale state, and repeated cleanup.

## 7. Runtime Validation and Wrap Up

- [ ] 7.1 Smoke-test Docker Desktop on macOS amd64 and arm64 with dashboard access, discovery, reachability, failover, and cleanup.
- [ ] 7.2 Smoke-test Docker Desktop on Windows amd64, including a compatible pre-existing WinNAT and incompatible-state failure safety.
- [x] 7.3 Smoke-test Colima 0.10.3 on its unchanged default shared network and TCP-only SSH forwarder, with no `socket_vmnet` or profile edits.
- [x] 7.4 Run Go race tests and vet, shell/workflow validation, cross-compilation, image builds, and acceptance-criteria review.
- [ ] 7.5 Remove temporary fixtures and debugging state, confirm unrelated host networking remains intact, and prepare concise reviewer notes.
