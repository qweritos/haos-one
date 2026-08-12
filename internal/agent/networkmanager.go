package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"sort"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
)

const (
	nmBusName        = "org.freedesktop.NetworkManager"
	nmRootPath       = dbus.ObjectPath("/org/freedesktop/NetworkManager")
	nmSettingsPath   = dbus.ObjectPath("/org/freedesktop/NetworkManager/Settings")
	nmDNSPath        = dbus.ObjectPath("/org/freedesktop/NetworkManager/DnsManager")
	nmDevicePath     = dbus.ObjectPath("/org/freedesktop/NetworkManager/Devices/1")
	nmActivePath     = dbus.ObjectPath("/org/freedesktop/NetworkManager/ActiveConnection/1")
	nmConnectionPath = dbus.ObjectPath("/org/freedesktop/NetworkManager/Settings/1")
	nmIP4Path        = dbus.ObjectPath("/org/freedesktop/NetworkManager/IP4Config/1")
	nmIP6Path        = dbus.ObjectPath("/org/freedesktop/NetworkManager/IP6Config/1")
	projectionPath   = "/run/haos-one-net/network.json"
)

type networkProfile struct {
	Interface   string
	Address     string
	Prefix      uint32
	Gateway     string
	Nameservers []string
}

type projectionFile struct {
	Version     int      `json:"version"`
	Interface   string   `json:"interface"`
	Address     string   `json:"address"`
	Prefix      int      `json:"prefix"`
	Gateway     string   `json:"gateway"`
	Nameservers []string `json:"nameservers"`
	UpdatedUnix int64    `json:"updated_unix"`
}

func loadNetworkProfile(path string, now time.Time) networkProfile {
	fallback := networkProfile{Interface: "eth0", Address: "192.168.1.100", Prefix: 24, Gateway: "192.168.1.1", Nameservers: []string{"192.168.1.1"}}
	payload, err := os.ReadFile(path)
	if err != nil {
		return fallback
	}
	var projection projectionFile
	if json.Unmarshal(payload, &projection) != nil || projection.Version != 1 || projection.Interface != "haoswg0" || projection.Prefix < 0 || projection.Prefix > 32 || now.Unix()-projection.UpdatedUnix > 15 || now.Unix() < projection.UpdatedUnix {
		return fallback
	}
	if net.ParseIP(projection.Address).To4() == nil || net.ParseIP(projection.Gateway).To4() == nil {
		return fallback
	}
	profile := networkProfile{Interface: projection.Interface, Address: projection.Address, Prefix: uint32(projection.Prefix), Gateway: projection.Gateway}
	for _, resolver := range projection.Nameservers {
		if net.ParseIP(resolver) != nil {
			profile.Nameservers = append(profile.Nameservers, resolver)
		}
	}
	return profile
}

type propertySource func() map[string]dbus.Variant

type propertiesService struct {
	interfaceName string
	source        propertySource
}

func (service *propertiesService) Get(interfaceName, property string) (dbus.Variant, *dbus.Error) {
	if interfaceName != service.interfaceName {
		return dbus.Variant{}, dbus.NewError("org.freedesktop.DBus.Error.UnknownInterface", []any{interfaceName})
	}
	value, ok := service.source()[property]
	if !ok {
		return dbus.Variant{}, dbus.NewError("org.freedesktop.DBus.Error.UnknownProperty", []any{property})
	}
	return value, nil
}

func (service *propertiesService) GetAll(interfaceName string) (map[string]dbus.Variant, *dbus.Error) {
	if interfaceName != service.interfaceName {
		return nil, dbus.NewError("org.freedesktop.DBus.Error.UnknownInterface", []any{interfaceName})
	}
	return service.source(), nil
}

func (service *propertiesService) Set(interfaceName, property string, _ dbus.Variant) *dbus.Error {
	return dbus.NewError("org.freedesktop.DBus.Error.PropertyReadOnly", []any{interfaceName + "." + property})
}

type nmRootMethods struct{}

func (*nmRootMethods) ActivateConnection(dbus.ObjectPath, dbus.ObjectPath, dbus.ObjectPath) (dbus.ObjectPath, *dbus.Error) {
	return "/", readOnlyDBusError()
}
func (*nmRootMethods) AddAndActivateConnection(map[string]map[string]dbus.Variant, dbus.ObjectPath, dbus.ObjectPath) (dbus.ObjectPath, dbus.ObjectPath, *dbus.Error) {
	return "/", "/", readOnlyDBusError()
}
func (*nmRootMethods) CheckConnectivity() (uint32, *dbus.Error) { return 4, nil }

type nmSettingsMethods struct{}

func (*nmSettingsMethods) AddConnection(map[string]map[string]dbus.Variant) (dbus.ObjectPath, *dbus.Error) {
	return "/", readOnlyDBusError()
}
func (*nmSettingsMethods) ReloadConnections() (bool, *dbus.Error) { return false, readOnlyDBusError() }

type nmConnectionMethods struct{ profile func() networkProfile }

func (methods *nmConnectionMethods) GetSettings() (map[string]map[string]dbus.Variant, *dbus.Error) {
	return connectionSettings(methods.profile()), nil
}
func (*nmConnectionMethods) Update(map[string]map[string]dbus.Variant) *dbus.Error {
	return readOnlyDBusError()
}
func (*nmConnectionMethods) Delete() *dbus.Error { return readOnlyDBusError() }

func readOnlyDBusError() *dbus.Error {
	return dbus.NewError("org.freedesktop.NetworkManager.Error.PermissionDenied", []any{"HAOS One compatibility NetworkManager is read-only"})
}

type dbusObject struct {
	path       dbus.ObjectPath
	iface      string
	methods    any
	properties []introspect.Property
	source     propertySource
}

type NetworkManagerService struct {
	ProjectionPath string
}

func (service *NetworkManagerService) Run(ctx context.Context, ready func()) error {
	connection, err := dbus.ConnectSystemBus()
	if err != nil {
		return fmt.Errorf("connect system D-Bus: %w", err)
	}
	defer connection.Close()
	reply, err := connection.RequestName(nmBusName, dbus.NameFlagDoNotQueue)
	if err != nil {
		return fmt.Errorf("request NetworkManager D-Bus name: %w", err)
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		return fmt.Errorf("NetworkManager D-Bus name is already owned: %s", reply)
	}
	path := service.ProjectionPath
	if path == "" {
		path = projectionPath
	}
	profile := func() networkProfile { return loadNetworkProfile(path, time.Now()) }
	objects := networkManagerObjects(profile)
	for _, object := range objects {
		if object.methods != nil {
			if err := connection.Export(object.methods, object.path, object.iface); err != nil {
				return err
			}
		}
		properties := &propertiesService{interfaceName: object.iface, source: object.source}
		if err := connection.Export(properties, object.path, "org.freedesktop.DBus.Properties"); err != nil {
			return err
		}
		interfaces := []introspect.Interface{{Name: object.iface, Properties: object.properties}}
		if object.methods != nil {
			interfaces[0].Methods = introspect.Methods(object.methods)
		}
		interfaces = append(interfaces, propertiesIntrospection)
		node := &introspect.Node{Name: string(object.path), Interfaces: interfaces}
		if err := connection.Export(introspect.NewIntrospectable(node), object.path, "org.freedesktop.DBus.Introspectable"); err != nil {
			return err
		}
	}
	if ready != nil {
		ready()
	}
	<-ctx.Done()
	return nil
}

func networkManagerObjects(profile func() networkProfile) []dbusObject {
	return []dbusObject{
		{nmRootPath, nmBusName, &nmRootMethods{}, props("Version", "s", "Connectivity", "u", "ConnectivityCheckEnabled", "b", "Devices", "ao", "PrimaryConnection", "o"), func() map[string]dbus.Variant {
			return map[string]dbus.Variant{"Version": variant("s", "1.42.0"), "Connectivity": variant("u", uint32(4)), "ConnectivityCheckEnabled": variant("b", true), "Devices": variant("ao", []dbus.ObjectPath{nmDevicePath}), "PrimaryConnection": variant("o", nmActivePath)}
		}},
		{nmSettingsPath, nmBusName + ".Settings", &nmSettingsMethods{}, props("Connections", "ao"), func() map[string]dbus.Variant {
			return map[string]dbus.Variant{"Connections": variant("ao", []dbus.ObjectPath{nmConnectionPath})}
		}},
		{nmConnectionPath, nmBusName + ".Settings.Connection", &nmConnectionMethods{profile: profile}, nil, func() map[string]dbus.Variant { return map[string]dbus.Variant{} }},
		{nmDevicePath, nmBusName + ".Device", nil, props("Interface", "s", "DeviceType", "u", "Driver", "s", "Managed", "b", "HwAddress", "s", "Path", "s", "ActiveConnection", "o"), func() map[string]dbus.Variant {
			current := profile()
			return map[string]dbus.Variant{"Interface": variant("s", current.Interface), "DeviceType": variant("u", uint32(1)), "Driver": variant("s", "dummy-eth"), "Managed": variant("b", true), "HwAddress": variant("s", "AA:BB:CC:DD:EE:01"), "Path": variant("s", "dummy-"+current.Interface), "ActiveConnection": variant("o", nmActivePath)}
		}},
		{nmActivePath, nmBusName + ".Connection.Active", nil, props("Connection", "o", "Id", "s", "Uuid", "s", "Type", "s", "State", "u", "StateFlags", "u", "Ip4Config", "o", "Ip6Config", "o"), func() map[string]dbus.Variant {
			current := profile()
			return map[string]dbus.Variant{"Connection": variant("o", nmConnectionPath), "Id": variant("s", "Supervisor "+current.Interface), "Uuid": variant("s", "00000000-0000-0000-0000-000000000001"), "Type": variant("s", "802-3-ethernet"), "State": variant("u", uint32(2)), "StateFlags": variant("u", uint32(0)), "Ip4Config": variant("o", nmIP4Path), "Ip6Config": variant("o", nmIP6Path)}
		}},
		{nmIP4Path, nmBusName + ".IP4Config", nil, props("AddressData", "aa{sv}", "Gateway", "s", "NameserverData", "aa{sv}"), func() map[string]dbus.Variant {
			current := profile()
			addressData := []map[string]dbus.Variant{{"address": variant("s", current.Address), "prefix": variant("u", current.Prefix)}}
			nameservers := make([]map[string]dbus.Variant, 0, len(current.Nameservers))
			for _, resolver := range current.Nameservers {
				nameservers = append(nameservers, map[string]dbus.Variant{"address": variant("s", resolver)})
			}
			return map[string]dbus.Variant{"AddressData": variant("aa{sv}", addressData), "Gateway": variant("s", current.Gateway), "NameserverData": variant("aa{sv}", nameservers)}
		}},
		{nmIP6Path, nmBusName + ".IP6Config", nil, props("AddressData", "aa{sv}", "Gateway", "s", "Nameservers", "aay"), func() map[string]dbus.Variant {
			addressData := []map[string]dbus.Variant{{"address": variant("s", "2001:db8::100"), "prefix": variant("u", uint32(64))}}
			return map[string]dbus.Variant{"AddressData": variant("aa{sv}", addressData), "Gateway": variant("s", "fe80::1"), "Nameservers": variant("aay", [][]byte{net.ParseIP("2001:4860:4860::8888").To16()})}
		}},
		{nmDNSPath, nmBusName + ".DnsManager", nil, props("Mode", "s", "RcManager", "s", "Configuration", "aa{sv}"), func() map[string]dbus.Variant {
			current := profile()
			configuration := []map[string]dbus.Variant{{"nameservers": variant("as", current.Nameservers), "domains": variant("as", []string{"local"}), "interface": variant("s", current.Interface), "priority": variant("i", int32(100)), "vpn": variant("b", false)}}
			return map[string]dbus.Variant{"Mode": variant("s", "default"), "RcManager": variant("s", "file"), "Configuration": variant("aa{sv}", configuration)}
		}},
	}
}

func connectionSettings(profile networkProfile) map[string]map[string]dbus.Variant {
	return map[string]map[string]dbus.Variant{
		"connection":     {"id": variant("s", "Supervisor "+profile.Interface), "uuid": variant("s", "00000000-0000-0000-0000-000000000001"), "type": variant("s", "802-3-ethernet"), "interface-name": variant("s", profile.Interface), "llmnr": variant("i", int32(2)), "mdns": variant("i", int32(2))},
		"ipv4":           {"method": variant("s", "auto"), "address-data": variant("aa{sv}", []map[string]dbus.Variant{{"address": variant("s", profile.Address), "prefix": variant("u", profile.Prefix)}}), "gateway": variant("s", profile.Gateway)},
		"ipv6":           {"method": variant("s", "auto"), "addr-gen-mode": variant("i", int32(0)), "ip6-privacy": variant("i", int32(0))},
		"802-3-ethernet": {"assigned-mac-address": variant("s", "preserve")},
	}
}

func variant(signature string, value any) dbus.Variant {
	parsed, err := dbus.ParseSignature(signature)
	if err != nil {
		panic(err)
	}
	return dbus.MakeVariantWithSignature(value, parsed)
}

func props(values ...string) []introspect.Property {
	properties := make([]introspect.Property, 0, len(values)/2)
	for index := 0; index < len(values); index += 2 {
		properties = append(properties, introspect.Property{Name: values[index], Type: values[index+1], Access: "read"})
	}
	sort.Slice(properties, func(i, j int) bool { return properties[i].Name < properties[j].Name })
	return properties
}

var propertiesIntrospection = introspect.Interface{Name: "org.freedesktop.DBus.Properties", Methods: []introspect.Method{
	{Name: "Get", Args: []introspect.Arg{{Name: "interface", Type: "s", Direction: "in"}, {Name: "property", Type: "s", Direction: "in"}, {Name: "value", Type: "v", Direction: "out"}}},
	{Name: "GetAll", Args: []introspect.Arg{{Name: "interface", Type: "s", Direction: "in"}, {Name: "properties", Type: "a{sv}", Direction: "out"}}},
	{Name: "Set", Args: []introspect.Arg{{Name: "interface", Type: "s", Direction: "in"}, {Name: "property", Type: "s", Direction: "in"}, {Name: "value", Type: "v", Direction: "in"}}},
}}
