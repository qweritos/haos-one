//go:build darwin

package netagent

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
)

func createTunnelInterface(ctx context.Context, cfg *Config) (string, *exec.Cmd, error) {
	helper, err := findHelper()
	if err != nil {
		return "", nil, err
	}
	dir, err := os.MkdirTemp("", "haos-one-wg-")
	if err != nil {
		return "", nil, err
	}
	nameFile := filepath.Join(dir, "name")
	cmd := exec.CommandContext(ctx, helper, "-f", "utun")
	cmd.Env = append(os.Environ(), "WG_TUN_NAME_FILE="+nameFile)
	if err := cmd.Start(); err != nil {
		_ = os.RemoveAll(dir)
		return "", nil, err
	}
	name, err := waitForFile(ctx, nameFile)
	_ = os.RemoveAll(dir)
	if err != nil {
		_ = cmd.Process.Kill()
		return "", nil, err
	}
	return name, cmd, nil
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
