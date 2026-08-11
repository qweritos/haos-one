//go:build linux

package netagent

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
)

func createTunnelInterface(ctx context.Context, cfg *Config) (string, *exec.Cmd, error) {
	name := tunnelInterfaceName(cfg.Role)
	if err := runCommand(ctx, "ip", "link", "show", "dev", name); err == nil {
		if _, markerErr := os.Stat(linuxOwnerMarker); markerErr != nil {
			return "", nil, fmt.Errorf("interface %s already exists and is not marked as HAOS One-owned", name)
		}
		if err := runCommand(ctx, "ip", "link", "delete", name); err != nil {
			return "", nil, err
		}
	}
	if err := runCommand(ctx, "ip", "link", "add", name, "type", "wireguard"); err == nil {
		if markerErr := writeLinuxOwnerMarker(); markerErr != nil {
			_ = runCommand(ctx, "ip", "link", "delete", name)
			return "", nil, markerErr
		}
		return name, nil, nil
	}
	helper, err := findHelper()
	if err != nil {
		return "", nil, fmt.Errorf("kernel WireGuard unavailable and fallback failed: %w", err)
	}
	cmd := exec.CommandContext(ctx, helper, "-f", name)
	if err := cmd.Start(); err != nil {
		return "", nil, err
	}
	if markerErr := writeLinuxOwnerMarker(); markerErr != nil {
		_ = cmd.Process.Kill()
		return "", nil, markerErr
	}
	return name, cmd, nil
}

func configureInterfaceAddress(ctx context.Context, name string, cfg *Config) error {
	prefix := strings.Split(cfg.TunnelCIDR, "/")[1]
	if err := runCommand(ctx, "ip", "address", "add", cfg.Address+"/"+prefix, "dev", name); err != nil {
		return err
	}
	return runCommand(ctx, "ip", "link", "set", "dev", name, "mtu", fmt.Sprint(cfg.MTU), "up")
}

func removeTunnelInterface(ctx context.Context, name string) error {
	err := runCommand(ctx, "ip", "link", "delete", name)
	_ = os.Remove(linuxOwnerMarker)
	return err
}

const linuxOwnerMarker = "/run/haos-one-net/wireguard.owner"

func writeLinuxOwnerMarker() error {
	if err := os.MkdirAll("/run/haos-one-net", 0o700); err != nil {
		return err
	}
	return os.WriteFile(linuxOwnerMarker, []byte("haoswg0\n"), 0o600)
}

func pinEndpointRoute(ctx context.Context, ip net.IP) error {
	out, err := commandOutput(ctx, "ip", "-4", "route", "show", "default")
	if err != nil {
		return err
	}
	fields := strings.Fields(strings.Split(out, "\n")[0])
	args := []string{"route", "replace", ip.String() + "/32"}
	for i, field := range fields {
		if (field == "via" || field == "dev") && i+1 < len(fields) {
			args = append(args, field, fields[i+1])
		}
	}
	return runCommand(ctx, "ip", args...)
}

func unpinEndpointRoute(ctx context.Context, ip net.IP) error {
	return runCommand(ctx, "ip", "route", "delete", ip.String()+"/32")
}
