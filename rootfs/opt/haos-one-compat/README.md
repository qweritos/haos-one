# Dummy NetworkManager D-Bus Service

This is a tiny, fake NetworkManager D-Bus responder. 
It implements only the NetworkManager methods/properties that
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
