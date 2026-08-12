//go:build darwin

package netagent

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

const pfAnchor = "com.apple/haos-one"

func prepareHost(ctx context.Context, cfg *Config, tunnel *Tunnel) (*State, error) {
	state := &State{Version: ConfigVersion, Platform: "darwin", Interface: tunnel.Name, PFAnchor: pfAnchor}
	success := false
	defer func() {
		if !success {
			_ = cleanupHost(context.Background(), state)
		}
	}()
	forwarding, err := commandOutput(ctx, "sysctl", "-n", "net.inet.ip.forwarding")
	if err != nil {
		return nil, err
	}
	state.ForwardingWasOn = strings.TrimSpace(forwarding) == "1"
	if !state.ForwardingWasOn {
		if err := runCommand(ctx, "sysctl", "-w", "net.inet.ip.forwarding=1"); err != nil {
			return nil, err
		}
		state.ForwardingChanged = true
	}
	enableOutput, err := commandCombinedOutput(ctx, "pfctl", "-E")
	if err != nil {
		return nil, err
	}
	re := regexp.MustCompile(`(?i)token\s*:\s*([0-9]+)`)
	if match := re.FindStringSubmatch(enableOutput); len(match) == 2 {
		state.PFToken = match[1]
	}
	var rules strings.Builder
	for _, iface := range cfg.Interfaces {
		fmt.Fprintf(&rules, "nat on %s inet from %s to any -> (%s)\n", iface, cfg.TunnelCIDR, iface)
		fmt.Fprintf(&rules, "pass out quick on %s inet from %s to any keep state\n", iface, cfg.TunnelCIDR)
		fmt.Fprintf(&rules, "pass in quick on %s inet proto tcp to (%s) port %d keep state\n", iface, iface, cfg.EffectiveHTTPPort())
	}
	fmt.Fprintf(&rules, "pass in quick on %s inet from %s to any keep state\n", tunnel.Name, cfg.TunnelCIDR)
	fmt.Fprintf(&rules, "pass in quick inet proto udp to any port %d keep state\n", cfg.ListenPort)
	dir, err := os.MkdirTemp("", "haos-one-pf-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "rules.conf")
	if err := os.WriteFile(path, []byte(rules.String()), 0o600); err != nil {
		return nil, err
	}
	if err := runCommand(ctx, "pfctl", "-a", pfAnchor, "-f", path); err != nil {
		return nil, fmt.Errorf("load PF anchor: %w", err)
	}
	if err := SaveState(cfg.StateFile, state); err != nil {
		return nil, err
	}
	success = true
	return state, nil
}

func cleanupHost(ctx context.Context, state *State) error {
	var first error
	if state.PFAnchor != "" {
		if err := runCommand(ctx, "pfctl", "-a", state.PFAnchor, "-F", "all"); err != nil {
			first = err
		}
	}
	if state.PFToken != "" {
		_ = runCommand(ctx, "pfctl", "-X", state.PFToken)
	}
	if state.ForwardingChanged {
		if err := runCommand(ctx, "sysctl", "-w", "net.inet.ip.forwarding=0"); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func addGuestRoutes(ctx context.Context, tunnel string, cidrs []string) error {
	return fmt.Errorf("guest routes are unsupported on macOS")
}

func removeGuestRoutes(ctx context.Context, tunnel string, cidrs []string) error {
	return nil
}

func injectUDP(source net.IP, sourcePort int, destination net.IP, destinationPort int, ttl int, payload []byte, interfaceName string) error {
	return fmt.Errorf("packet injection is only supported by the Linux guest")
}

func isAdministrator() bool { return os.Geteuid() == 0 }

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func stopOwnedHelper(ctx context.Context, state *State) error {
	if state.HelperPID <= 0 {
		return nil
	}
	out, err := commandOutput(ctx, "ps", "-p", fmt.Sprint(state.HelperPID), "-o", "command=")
	if err != nil || !strings.Contains(out, "wireguard-go") {
		return nil
	}
	process, err := os.FindProcess(state.HelperPID)
	if err != nil {
		return err
	}
	return process.Kill()
}
