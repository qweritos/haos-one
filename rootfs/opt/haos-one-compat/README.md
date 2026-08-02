# haos_one_compat

This container runs two compatibility shims:

- a tiny fake NetworkManager D-Bus responder
- a Docker UNIX-socket proxy that filters `/containers/json`, lightly tags `/info`,
  and removes nested-LXC-incompatible options from container-create requests

The dummy NetworkManager service implements only the methods and properties that
Supervisor uses, with static data and minimal state updates.

Run on the session bus (default):
  python dummy-nm.py

To have Supervisor use the session bus instead of the system bus:
  export DBUS_SYSTEM_BUS_ADDRESS="$DBUS_SESSION_BUS_ADDRESS"

Run on the system bus (requires permissions):
  python dummy-nm/service.py --bus system

## methods used

  - org.freedesktop.NetworkManager: ActivateConnection, AddAndActivateConnection, CheckConnectivity
  - org.freedesktop.NetworkManager.Settings: AddConnection, ReloadConnections
  - org.freedesktop.NetworkManager.Settings.Connection: GetSettings, Update, Delete (+ Updated/Removed signals)
  - org.freedesktop.NetworkManager.Device.Wireless: RequestScan, GetAllAccessPoints
  - org.freedesktop.NetworkManager.Connection.Active: StateChanged signal (properties for state, config paths,
    etc.)
