package netagent

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
	"gopkg.in/yaml.v3"
)

const (
	ConfigVersion       = 1
	DefaultListenPort   = 51821
	DefaultRelayPort    = 47821
	DefaultMTU          = 1280
	DefaultHostEndpoint = "host.docker.internal"
	DefaultTunnelCIDR   = "10.203.0.0/30"
	DefaultConfigName   = "desktop-network.yaml"
)

type Config struct {
	Version       int      `yaml:"version"`
	Role          string   `yaml:"role"`
	Runtime       string   `yaml:"runtime"`
	PrivateKey    string   `yaml:"private_key"`
	PeerPublicKey string   `yaml:"peer_public_key"`
	TunnelCIDR    string   `yaml:"tunnel_cidr"`
	Address       string   `yaml:"address"`
	PeerAddress   string   `yaml:"peer_address"`
	ListenPort    int      `yaml:"listen_port,omitempty"`
	RelayPort     int      `yaml:"relay_port"`
	MTU           int      `yaml:"mtu"`
	HostEndpoint  string   `yaml:"host_endpoint,omitempty"`
	AutoInterface bool     `yaml:"auto_interface,omitempty"`
	AutoCIDRs     bool     `yaml:"auto_cidrs,omitempty"`
	Interfaces    []string `yaml:"interfaces,omitempty"`
	LANCIDRs      []string `yaml:"lan_cidrs,omitempty"`
	StateFile     string   `yaml:"state_file,omitempty"`
}

type State struct {
	Version             int       `yaml:"version"`
	Platform            string    `yaml:"platform"`
	Interface           string    `yaml:"interface"`
	HelperPID           int       `yaml:"helper_pid,omitempty"`
	ForwardingWasOn     bool      `yaml:"forwarding_was_on,omitempty"`
	ForwardingChanged   bool      `yaml:"forwarding_changed,omitempty"`
	PFAnchor            string    `yaml:"pf_anchor,omitempty"`
	PFToken             string    `yaml:"pf_token,omitempty"`
	WindowsNAT          string    `yaml:"windows_nat,omitempty"`
	WindowsNATCreated   bool      `yaml:"windows_nat_created,omitempty"`
	WindowsFirewallRule string    `yaml:"windows_firewall_rule,omitempty"`
	Routes              []string  `yaml:"routes,omitempty"`
	LastGuestHeartbeat  time.Time `yaml:"last_guest_heartbeat,omitempty"`
	RelaySent           uint64    `yaml:"relay_sent,omitempty"`
	RelayReceived       uint64    `yaml:"relay_received,omitempty"`
	MDNSRelayed         uint64    `yaml:"mdns_relayed,omitempty"`
	SSDPRelayed         uint64    `yaml:"ssdp_relayed,omitempty"`
	DuplicatesDropped   uint64    `yaml:"duplicates_dropped,omitempty"`
}

func DefaultConfigDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "haos-one", "net"), nil
}

func LoadConfig(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid %s: %w", path, err)
	}
	return &cfg, nil
}

func SaveConfig(path string, cfg *Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	b, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		username := os.Getenv("USERNAME")
		if username == "" {
			return errors.New("USERNAME is unset; cannot protect generated keys")
		}
		if out, err := exec.Command("icacls.exe", path, "/inheritance:r", "/grant:r", username+":(F)").CombinedOutput(); err != nil {
			return fmt.Errorf("protect %s ACL: %w: %s", path, err, strings.TrimSpace(string(out)))
		}
	}
	return nil
}

func LoadState(path string) (*State, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var state State
	if err := yaml.Unmarshal(b, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

func SaveState(path string, state *State) error {
	b, err := yaml.Marshal(state)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

func (c *Config) Validate() error {
	if c.Version != ConfigVersion {
		return fmt.Errorf("unsupported version %d", c.Version)
	}
	if c.Role != "host" && c.Role != "guest" {
		return errors.New("role must be host or guest")
	}
	if c.Runtime != "docker-desktop" && c.Runtime != "colima" {
		return errors.New("runtime must be docker-desktop or colima")
	}
	if c.PrivateKey == "" || c.PeerPublicKey == "" {
		return errors.New("private_key and peer_public_key are required")
	}
	if _, err := wgtypes.ParseKey(c.PrivateKey); err != nil {
		return fmt.Errorf("invalid private_key: %w", err)
	}
	if _, err := wgtypes.ParseKey(c.PeerPublicKey); err != nil {
		return fmt.Errorf("invalid peer_public_key: %w", err)
	}
	hostIP, guestIP, err := TunnelAddresses(c.TunnelCIDR)
	if err != nil {
		return err
	}
	for name, value := range map[string]string{"address": c.Address, "peer_address": c.PeerAddress} {
		if net.ParseIP(value).To4() == nil {
			return fmt.Errorf("%s must be an IPv4 address", name)
		}
	}
	if (c.Role == "host" && (!net.ParseIP(c.Address).Equal(hostIP) || !net.ParseIP(c.PeerAddress).Equal(guestIP))) ||
		(c.Role == "guest" && (!net.ParseIP(c.Address).Equal(guestIP) || !net.ParseIP(c.PeerAddress).Equal(hostIP))) {
		return errors.New("address and peer_address must be the .1/.2 usable addresses of tunnel_cidr")
	}
	if c.ListenPort < 0 || c.ListenPort > 65535 || c.RelayPort < 1 || c.RelayPort > 65535 {
		return errors.New("invalid listen or relay port")
	}
	if c.Role == "host" && c.ListenPort == 0 {
		return errors.New("host listen_port is required")
	}
	if c.MTU < 576 || c.MTU > 9000 {
		return errors.New("mtu must be between 576 and 9000")
	}
	if c.Role == "guest" && strings.TrimSpace(c.HostEndpoint) == "" {
		return errors.New("host_endpoint is required for guest")
	}
	if c.Role == "guest" {
		if _, _, err := net.SplitHostPort(c.HostEndpoint); err != nil {
			return fmt.Errorf("invalid host_endpoint: %w", err)
		}
	}
	if c.Role == "host" && (len(c.Interfaces) == 0 || c.StateFile == "") {
		return errors.New("host interfaces and state_file are required")
	}
	if len(c.LANCIDRs) == 0 {
		return errors.New("at least one lan_cidr is required")
	}
	for _, cidr := range c.LANCIDRs {
		ip, _, err := net.ParseCIDR(cidr)
		if err != nil || ip.To4() == nil {
			return fmt.Errorf("invalid IPv4 lan_cidr %q", cidr)
		}
	}
	return nil
}

func DefaultStateFile(configDir string) string {
	return filepath.Join(configDir, "state.yaml")
}

func HelperName() string {
	if runtime.GOOS == "windows" {
		return "wireguard-go.exe"
	}
	return "wireguard-go"
}
