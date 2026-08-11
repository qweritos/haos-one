package netagent

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"golang.zx2c4.com/wireguard/wgctrl"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

type Tunnel struct {
	Name       string
	Helper     *exec.Cmd
	Config     *Config
	EndpointIP net.IP
}

func StartTunnel(ctx context.Context, cfg *Config) (*Tunnel, error) {
	name, helper, err := createTunnelInterface(ctx, cfg)
	if err != nil {
		return nil, err
	}
	tunnel := &Tunnel{Name: name, Helper: helper, Config: cfg}
	cleanupOnError := func(err error) (*Tunnel, error) {
		_ = tunnel.Close(context.Background())
		return nil, err
	}
	if err := waitWireGuardDevice(ctx, name); err != nil {
		return cleanupOnError(err)
	}
	if err := configureInterfaceAddress(ctx, name, cfg); err != nil {
		return cleanupOnError(err)
	}
	privateKey, err := wgtypes.ParseKey(cfg.PrivateKey)
	if err != nil {
		return cleanupOnError(fmt.Errorf("private key: %w", err))
	}
	peerKey, err := wgtypes.ParseKey(cfg.PeerPublicKey)
	if err != nil {
		return cleanupOnError(fmt.Errorf("peer public key: %w", err))
	}
	peer := wgtypes.PeerConfig{PublicKey: peerKey, ReplaceAllowedIPs: true}
	peer.AllowedIPs = []net.IPNet{{IP: net.ParseIP(cfg.PeerAddress).To4(), Mask: net.CIDRMask(32, 32)}}
	listenPort := cfg.ListenPort
	if cfg.Role == "guest" {
		for _, value := range cfg.LANCIDRs {
			_, network, parseErr := net.ParseCIDR(value)
			if parseErr == nil {
				peer.AllowedIPs = append(peer.AllowedIPs, *network)
			}
		}
		endpoint, endpointIP, resolveErr := ResolveEndpoint(cfg.HostEndpoint)
		if resolveErr != nil {
			return cleanupOnError(resolveErr)
		}
		if err := pinEndpointRoute(ctx, endpointIP); err != nil {
			return cleanupOnError(fmt.Errorf("pin WireGuard endpoint route: %w", err))
		}
		tunnel.EndpointIP = endpointIP
		peer.Endpoint = endpoint
		keepalive := 15 * time.Second
		peer.PersistentKeepaliveInterval = &keepalive
		listenPort = 0
	}
	client, err := wgctrl.New()
	if err != nil {
		return cleanupOnError(err)
	}
	defer client.Close()
	replacePeers := true
	wgConfig := wgtypes.Config{PrivateKey: &privateKey, ReplacePeers: replacePeers, Peers: []wgtypes.PeerConfig{peer}}
	if cfg.Role == "host" {
		wgConfig.ListenPort = &listenPort
	}
	if err := client.ConfigureDevice(name, wgConfig); err != nil {
		return cleanupOnError(fmt.Errorf("configure WireGuard interface %s: %w", name, err))
	}
	return tunnel, nil
}

func waitWireGuardDevice(ctx context.Context, name string) error {
	timeout := time.NewTimer(5 * time.Second)
	defer timeout.Stop()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		client, err := wgctrl.New()
		if err == nil {
			_, err = client.Device(name)
			_ = client.Close()
			if err == nil {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout.C:
			return fmt.Errorf("timed out waiting for WireGuard device %s", name)
		case <-ticker.C:
		}
	}
}

func ResolveEndpoint(value string) (*net.UDPAddr, net.IP, error) {
	host, port, err := net.SplitHostPort(value)
	if err != nil {
		return nil, nil, fmt.Errorf("host_endpoint %q: %w", value, err)
	}
	addresses, err := net.LookupIP(host)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve %s: %w", host, err)
	}
	for _, address := range addresses {
		if ip := address.To4(); ip != nil {
			udp, err := net.ResolveUDPAddr("udp4", net.JoinHostPort(ip.String(), port))
			return udp, ip, err
		}
	}
	return nil, nil, fmt.Errorf("%s has no IPv4 address", host)
}

func (t *Tunnel) RefreshEndpoint(ctx context.Context) error {
	if t.Config.Role != "guest" {
		return nil
	}
	endpoint, ip, err := ResolveEndpoint(t.Config.HostEndpoint)
	if err != nil {
		return err
	}
	if t.EndpointIP != nil && t.EndpointIP.Equal(ip) {
		return nil
	}
	if err := pinEndpointRoute(ctx, ip); err != nil {
		return err
	}
	key, err := wgtypes.ParseKey(t.Config.PeerPublicKey)
	if err != nil {
		return err
	}
	client, err := wgctrl.New()
	if err != nil {
		return err
	}
	defer client.Close()
	if err := client.ConfigureDevice(t.Name, wgtypes.Config{Peers: []wgtypes.PeerConfig{{PublicKey: key, UpdateOnly: true, Endpoint: endpoint}}}); err != nil {
		_ = unpinEndpointRoute(ctx, ip)
		return err
	}
	if t.EndpointIP != nil {
		_ = unpinEndpointRoute(ctx, t.EndpointIP)
	}
	t.EndpointIP = ip
	return nil
}

func (t *Tunnel) HandshakeAge() (time.Duration, error) {
	client, err := wgctrl.New()
	if err != nil {
		return 0, err
	}
	defer client.Close()
	device, err := client.Device(t.Name)
	if err != nil {
		return 0, err
	}
	if len(device.Peers) == 0 || device.Peers[0].LastHandshakeTime.IsZero() {
		return 0, nil
	}
	return time.Since(device.Peers[0].LastHandshakeTime), nil
}

func (t *Tunnel) UpdateAllowedIPs(cidrs []string) error {
	key, err := wgtypes.ParseKey(t.Config.PeerPublicKey)
	if err != nil {
		return err
	}
	allowed := []net.IPNet{{IP: net.ParseIP(t.Config.PeerAddress).To4(), Mask: net.CIDRMask(32, 32)}}
	for _, value := range cidrs {
		_, network, parseErr := net.ParseCIDR(value)
		if parseErr != nil || network.IP.To4() == nil {
			return fmt.Errorf("invalid LAN CIDR %q", value)
		}
		allowed = append(allowed, *network)
	}
	client, err := wgctrl.New()
	if err != nil {
		return err
	}
	defer client.Close()
	return client.ConfigureDevice(t.Name, wgtypes.Config{Peers: []wgtypes.PeerConfig{{PublicKey: key, UpdateOnly: true, ReplaceAllowedIPs: true, AllowedIPs: allowed}}})
}

func (t *Tunnel) Close(ctx context.Context) error {
	if t.EndpointIP != nil {
		_ = unpinEndpointRoute(ctx, t.EndpointIP)
	}
	err := removeTunnelInterface(ctx, t.Name)
	if t.Helper != nil && t.Helper.Process != nil {
		_ = t.Helper.Process.Kill()
		_, _ = t.Helper.Process.Wait()
	}
	return err
}

func findHelper() (string, error) {
	executable, err := os.Executable()
	if err == nil {
		candidate := filepath.Join(filepath.Dir(executable), HelperName())
		if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	path, err := exec.LookPath(HelperName())
	if err != nil {
		return "", fmt.Errorf("%s is required next to haos-one-net or in PATH", HelperName())
	}
	return path, nil
}

func waitForFile(ctx context.Context, path string) (string, error) {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	timeout := time.NewTimer(5 * time.Second)
	defer timeout.Stop()
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-timeout.C:
			return "", fmt.Errorf("timed out waiting for %s", path)
		case <-ticker.C:
			b, err := os.ReadFile(path)
			if err == nil && strings.TrimSpace(string(b)) != "" {
				return strings.TrimSpace(string(b)), nil
			}
		}
	}
}

func tunnelInterfaceName(role string) string {
	if runtime.GOOS == "windows" {
		return "haos-one-" + role
	}
	return "haoswg0"
}
