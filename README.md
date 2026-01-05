<div align="center">
  <h1><img alt="Home Assistant" height="48" src="https://cdn.simpleicons.org/homeassistant/41BDF5" /> haos-one</h1>
  <p>Home Assistant Operating System <br /> Single‑Container Docker Image</p>
  <h2>✨ Full HAOS Vibes, Inside Docker ✨</h2>
  <p>Run a fully featured HAOS instance in a single Docker container</p>
  <p>Keep the same experience you’d get on dedicated hardware or a VM.</p>
  <p>
    <h4>
    <a href="https://github.com/hassio-addons">
      <img alt="Add-ons"  src="https://avatars.githubusercontent.com/u/30772201?s=16&v=4" />
      Add-ons supported, no compromises.
    </a>
    </h4>
    <strong>🚧 Work In Progress 🚧</strong>
  </p>
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

- [Run HA OS without sacrificing a whole computer to it.](https://www.home-assistant.io/blog/2025/05/22/deprecating-core-and-supervised-installation-methods-and-32-bit-systems/)
- Avoid VM performance overhead and hypervisor complexity.
- Use host hardware (like USB devices) directly without passthrough.
- Use host networking for service autodiscovery, simpler routing, and lower latency.
- __x86_64__ and __aarch64__ images available.
- Rootless containers support (experimental)
- Kubernetes? Sure. [Helm chart included](./charts/haos-one).

## How

Simple as one command:

```
docker run --name haos -ti --privileged -p 8123:8123 -v ./data:/mnt/data qweritos/haos-one
```

<p align="">
  <img alt="Intro" src="docs/assets/intro.webp" />
</p>

Your persistence (configuration and other data) is stored in `./data/`.

Replace `-p 8123:8123` with `--network host` if you want host networking (required for autodiscovery features).

Wait for http://localhost:8123 to be available. Now you can create new House or restore from existing backup.

> First startup can take a while as it pulls all required images — please be patient.

## Migration from deprecated Supervised installation method

### Method 1: Backup restore

- Create a full backup in your existing install (Settings → System → Backups).
- Download the backup to your host.
- Start this container and restore the backup during onboarding.
- Confirm add-ons and integrations come back after restore.

### Method 2: Manual `/usr/share/hassio` copy via host

Making consistent 1:1 clone of your HA instance.

> All commands to be executed from host

Get your `/usr/share/hassio` contents from existing Supervised installation:

```bash
cp -r /usr/share/hassio ./old-config
```

then, push it to new instance:

```bash
docker exec -it haos sh -c 'mv /mnt/data/supervisor /mnt/data/supervisor.bak && mkdir -p /mnt/data/supervisor'
docker cp ./old-config/. haos:/mnt/data/supervisor/
```

Finally, restart all Home Assistant containers:

```bash
docker exec -it haos systemctl restart docker
```

## Recipes

- Host networking (best for autodiscovery):
  ```
  docker run --name haos -ti --privileged --network host -v ./data:/mnt/data qweritos/haos-one
  ```
- macOS: use a named volume (overlay2 feature gaps with bind mounts):
  ```
  docker volume create haos-data
  docker run --name haos -ti --privileged -p 8123:8123 -v haos-data:/mnt/data qweritos/haos-one
  ```

## Troubleshooting

- Drop into HA CLI: `docker attach haos`
  > (detach with `Ctrl-p` + `Ctrl-q`) — more details: [here](https://docs.docker.com/reference/cli/docker/container/attach/#attach-to-and-detach-from-a-running-container)
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

| OS                             | Arch   | Env                                                         | Status | Notes                |
| ------------------------------ | ------ | ----------------------------------------------------------- | ------ | -------------------- |
| macOS 15.6 (24G84)             | x86_64 | Docker Desktop 4.55.0, Docker Engine 29.1.3 (client/server) | ✅     | AppArmor unavailable; use named volume (see [Recipes](#recipes)). |
| Ubuntu 25.10 (Questing Quokka) | x86_64 | Docker Engine 29.1.3 (client/server) <br />*(rootless & rootfull)*                        | ✅     | —                    |
| Ubuntu 25.10 (Questing Quokka) | x86_64 | Podman 5.4.2                                                | ✅     | —                    |
| Armbian OS 25.02.0 (bullseye) | aarch64 | Docker Engine 28.0.0 (client/server) | ✅ | — |

## Known Issues

- `--network host` lets HA manage host networking and may cause misconfiguration.
- `"Unsupported system - Network Manager issues"` warning - fix in progress.
- `Failed to get outbound IP, retrying in 5s: can't get default interface from Supervisor: {"result":"error","message":"Interface default does not exist"` in journal (with non-host networking) - fix in progress.

## TODOs & Progress:

See [project page](https://github.com/users/qweritos/projects/2) for details.

## License

Apache License 2.0 (see `LICENSE`).

## Disclaimer

Not affiliated with [Home Assistant](https://github.com/home-assistant).
