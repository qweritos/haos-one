
<div align="center">
  <p>
    <img alt="Home Assistant" height="80" src="https://cdn.simpleicons.org/homeassistant/41BDF5" />
  </p>
  <h1>haos-one</h1>
  <p>Home Assistant Operating System <br /> Single‑Container Docker Image</p>
  <h2>✨ Full HAOS Vibes, Inside Docker ✨</h2>
  <p>Run a fully featured HAOS instance in a single Docker container</p>
  <p>Keep the same experience you’d get on dedicated hardware or a VM.</p>
</div>

<p align="center">
  <a href="https://github.com/qweritos/haos-one/releases"><img alt="Release" src="https://img.shields.io/github/v/release/qweritos/haos-one?style=flat-square" /></a>
  <a href="https://github.com/qweritos/haos-one/blob/main/LICENSE"><img alt="License" src="https://img.shields.io/github/license/qweritos/haos-one?style=flat-square" /></a>
  <a href="https://github.com/qweritos/haos-one/stargazers"><img alt="Stars" src="https://img.shields.io/github/stars/qweritos/haos-one?style=flat-square" /></a>
  <a href="https://github.com/qweritos/haos-one/forks"><img alt="Forks" src="https://img.shields.io/github/forks/qweritos/haos-one?style=flat-square" /></a>
  <a href="https://github.com/qweritos/haos-one/issues"><img alt="Issues" src="https://img.shields.io/github/issues/qweritos/haos-one?style=flat-square" /></a>
  <a href="https://github.com/qweritos/haos-one/commits/main"><img alt="Last Commit" src="https://img.shields.io/github/last-commit/qweritos/haos-one?style=flat-square" /></a>
</p>

<br />

- Run HAOS in a single container without a dedicated host or VM
- Avoid VM performance overhead and hypervisor complexity.
- Use host hardware (like USB devices) directly without passthrough.
- Use host networking for service autodiscovery, simpler routing, and lower latency.
- Kubernetes? Sure. [Helm chart included](./charts/haos-one).

## How
Simple as one command:
```
docker run --name haos -ti --privileged -p 8123:8123 -v ./data:/data qweritos/haos-one
```
Your persistence (configuration and other data) is stored in `./data/`.

Replace `-p 8123:8123` with `--network host` if you want host networking (required for autodiscovery features).

> First startup can take a while as it pulls all required images — please be patient.

## Troubleshooting
- Drop into HA CLI: `docker attach haos`
>  (detach with `Ctrl-p` + `Ctrl-q`) — more details: [here](https://docs.docker.com/reference/cli/docker/container/attach/#attach-to-and-detach-from-a-running-container)
- Systemd logs (incl. containers logs): `docker exec -it haos journalctl -xb`
- Container status: `docker exec -it haos docker ps -a`

## How it works

See [docs](docs) for details.

## Security Considerations
- Runs with `--privileged`, granting full access between host and container — see [more](https://docs.docker.com/enterprise/security/hardened-desktop/enhanced-container-isolation/#secured-privileged-containers).
- `--network host` exposes services directly on the host network.
- Protect `./data/` because it contains HA configuration and secrets.
- AppArmor may be unavailable depending on your environment.

## Tested Environments
| OS | Arch | Env | Status | Notes |
| --- | --- | --- | --- | --- |
| macOS 15.6 (24G84) | x86_64 | Docker Desktop 4.55.0, Docker Engine 29.1.3 (client/server) | ✅ | AppArmor unavailable |
| Ubuntu 25.10 (Questing Quokka) | x86_64 | Docker Engine 29.1.3 (client/server) | ✅ | — |

## Known Issues
- `--network host` lets HA manage host networking and may cause misconfiguration.
- `"Unsupported system - Network Manager issues"` warning - fix in progress.

## TODOs & Progress:
See [project page](https://github.com/users/qweritos/projects/2) for details.

## License
Apache License 2.0 (see `LICENSE`).

## Disclaimer
Not affiliated with [Home Assistant](https://github.com/home-assistant).
