## 1. Split Host and Guest Executables

- [x] 1.1 Rename the cross-platform command target to `cmd/haos-one-host`, flatten `host run` to `run`, and update usage, version output, configuration snippets, diagnostics, documentation, and tests without changing the existing YAML schema or keys.
- [x] 1.2 Add `cmd/haos-one-agent` as a Linux-only, no-subcommand daemon target and move guest-only tunnel, route, discovery injection, and projection entrypoints behind an internal agent API.
- [x] 1.3 Update the Dockerfile and systemd installation so images contain `haos-one-agent` and its one always-enabled service but do not contain `haos-one-host`; keep networking conditional on the read-only guest configuration mount.

## 2. Port Compatibility Services to Go

- [x] 2.1 Port Docker API request/response parsing and rewrite rules to Go with fixtures covering versioned endpoints, create mutations, compatibility-container filtering removal, chunked bodies, streaming, upgrades, and hijacked connections.
- [x] 2.2 Run the Go Docker proxy directly in the outer container, own `/run/docker.sock`, forward to `/run/docker-real.sock`, wait for upstream readiness, and preserve permissions and shutdown cleanup.
- [x] 2.3 Port the dummy NetworkManager D-Bus objects, properties, methods, and state generation to Go, including fake-Ethernet behavior and live/stale `haoswg0` projection.
- [x] 2.4 Port udev-shim mode detection and idempotent Supervisor migration to Go while retaining only the injected `udev-shim/sitecustomize.py` payload.
- [x] 2.5 Implement independent component supervision with bounded retry/backoff so Docker or D-Bus readiness and component failures do not block or terminate healthy networking components.
- [x] 2.6 Remove the Python compatibility package, `dbus-fast` dependency, nested compatibility Dockerfile/container lifecycle, shell migration helper, container-hiding rewrite, and obsolete environment/service wiring.

## 3. Packaging and Documentation

- [x] 3.1 Publish raw host artifacts as `haos-one-host-mac-intel`, `haos-one-host-mac-apple-silicon`, and `haos-one-host-windows.exe` with one `SHA256SUMS`, and update workflows to build the embedded Linux agent separately for amd64 and arm64 images.
- [x] 3.2 Update setup, upgrade, troubleshooting, systemd, Docker Run, Compose, and release documentation for the two roles and explicitly document preview configuration compatibility and the retained Python injection payload.
- [x] 3.3 Expose host and agent build/protocol versions in diagnostics and report incompatible host/guest protocol pairs.

## 4. Verification and Migration

- [x] 4.1 Convert existing Python compatibility tests into Go parity tests and add daemon lifecycle tests for missing configuration, delayed Docker/D-Bus readiness, component restart, and graceful shutdown.
- [x] 4.2 Run Go unit, race, and vet checks; cross-compile all host assets and both Linux agent architectures; validate systemd units, workflows, and image contents.
- [x] 4.3 Upgrade an existing configured Colima instance without rotating keys or data, verify HTTP ingress plus mDNS/SSDP, and confirm no compatibility image/container or Python daemon remains.
- [x] 4.4 Smoke-test the unified agent and renamed host binary on Docker Desktop for macOS and Windows, including Supervisor startup, fake NetworkManager state, udev-shim migration, cleanup, and helper recovery.
- [x] 4.5 Download the published prerelease assets, verify the aggregate checksums, pull both published image architectures, and prepare concise migration and reviewer notes.
