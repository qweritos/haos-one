package netagent

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"
)

const guestNetworkProjectionPath = "/run/haos-one-net/network.json"

type guestNetworkProjection struct {
	Version     int      `json:"version"`
	Interface   string   `json:"interface"`
	Address     string   `json:"address"`
	Prefix      int      `json:"prefix"`
	Gateway     string   `json:"gateway"`
	Nameservers []string `json:"nameservers,omitempty"`
	UpdatedUnix int64    `json:"updated_unix"`
}

func writeGuestNetworkProjection(path string, tunnel *Tunnel, now time.Time) error {
	_, network, err := net.ParseCIDR(tunnel.Config.TunnelCIDR)
	if err != nil {
		return err
	}
	prefix, _ := network.Mask.Size()
	projection := guestNetworkProjection{
		Version:     ConfigVersion,
		Interface:   tunnel.Name,
		Address:     tunnel.Config.Address,
		Prefix:      prefix,
		Gateway:     tunnel.Config.PeerAddress,
		UpdatedUnix: now.Unix(),
	}
	for _, resolver := range tunnel.BootstrapIPs {
		if ip := resolver.To4(); ip != nil {
			projection.Nameservers = append(projection.Nameservers, ip.String())
		}
	}
	payload, err := json.Marshal(projection)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, append(payload, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("publish guest network projection: %w", err)
	}
	return nil
}

func removeGuestNetworkProjection() {
	_ = os.Remove(guestNetworkProjectionPath)
}
