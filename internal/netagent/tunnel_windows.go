//go:build windows

package netagent

import (
	"context"
	"fmt"
	"net"
	"strings"
)

func createTunnelInterface(_ context.Context, cfg *Config) (string, tunnelHelper, error) {
	name := tunnelInterfaceName(cfg.Role)
	return startUserspaceWireGuard(name, cfg.MTU)
}

func configureInterfaceAddress(ctx context.Context, name string, cfg *Config) error {
	prefix := strings.Split(cfg.TunnelCIDR, "/")[1]
	ps := fmt.Sprintf("New-NetIPAddress -InterfaceAlias '%s' -IPAddress '%s' -PrefixLength %s -ErrorAction Stop | Out-Null; Set-NetIPInterface -InterfaceAlias '%s' -NlMtuBytes %d", psQuote(name), psQuote(cfg.Address), prefix, psQuote(name), cfg.MTU)
	return runCommand(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", ps)
}

func removeTunnelInterface(ctx context.Context, name string) error {
	ps := fmt.Sprintf("Get-NetAdapter -Name '%s' -ErrorAction SilentlyContinue | Disable-NetAdapter -Confirm:$false", psQuote(name))
	return runCommand(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", ps)
}

func pinEndpointRoute(ctx context.Context, ip net.IP) error {
	ps := fmt.Sprintf("$r=Get-NetRoute -DestinationPrefix '0.0.0.0/0' | Sort-Object RouteMetric | Select-Object -First 1; New-NetRoute -DestinationPrefix '%s/32' -InterfaceIndex $r.InterfaceIndex -NextHop $r.NextHop -ErrorAction Stop | Out-Null", ip.String())
	return runCommand(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", ps)
}

func unpinEndpointRoute(ctx context.Context, ip net.IP) error {
	ps := fmt.Sprintf("Get-NetRoute -DestinationPrefix '%s/32' -ErrorAction SilentlyContinue | Remove-NetRoute -Confirm:$false", ip.String())
	return runCommand(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", ps)
}
