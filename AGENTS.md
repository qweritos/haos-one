# Runtime Investigation

- Reproduce runtime issues locally before proposing fixes.
- For HA console and networking issues, start with `USE_DUMMY_NETWORKMANAGER=1`.
- Compare `docker run -ti` against `docker compose up` when behavior differs between launch methods.
- Prefer inspecting the live system before rebuilding:
  - `docker logs`
  - `docker exec ... systemctl status/show`
  - `docker exec ... journalctl`
  - `docker exec ... stty -F /dev/console size`
  - `docker exec ... ps -ef`
- For tty or console failures, verify `/dev/console` geometry and the stdin target of the `ha-cli@console.service` main process.
- If a runtime fix is small, validate it first in a live container by copying files in, running `systemctl daemon-reload`, and restarting only the affected service.
- Rebuild the image only after the runtime patch is understood or proven.
- Keep changes minimal and bias toward service overrides and wrapper scripts over invasive image changes.

# Deployment

- For development purposes, by default, push as `registry.andrey.wtf/haos:dev`.
- Never wait for helm chart to be ready.
- If asked to redeploy, build and push the docker image, then update the helm release. Resolve the existing release name first.

# Cost Safety

- Do not trigger paid CI, cloud builds, paid APIs, or other potentially billable services without explicit user approval.
