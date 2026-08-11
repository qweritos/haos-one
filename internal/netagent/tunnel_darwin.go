//go:build darwin

package netagent

import (
	"context"
	"fmt"
	"net"
)

func createTunnelInterface(_ context.Context, cfg *Config) (string, tunnelHelper, error) {
	return startUserspaceWireGuard("utun", cfg.MTU)
}

func configureInterfaceAddress(ctx context.Context, name string, cfg *Config) error {
	return runCommand(ctx, "ifconfig", name, "inet", cfg.Address, cfg.PeerAddress, "netmask", "255.255.255.252", "mtu", fmt.Sprint(cfg.MTU), "up")
}

func removeTunnelInterface(ctx context.Context, name string) error {
	// Closing wireguard-go removes an utun device; ifconfig destroy is unsupported.
	return nil
}

func pinEndpointRoute(ctx context.Context, ip net.IP) error {
	return runCommand(ctx, "route", "-n", "add", "-host", ip.String(), "-interface", defaultRouteInterface(ctx))
}

func unpinEndpointRoute(ctx context.Context, ip net.IP) error {
	return runCommand(ctx, "route", "-n", "delete", "-host", ip.String())
}

func defaultRouteInterface(ctx context.Context) string {
	name, err := DefaultInterface()
	if err != nil {
		return "en0"
	}
	return name
}
