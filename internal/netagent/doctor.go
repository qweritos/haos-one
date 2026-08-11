package netagent

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"golang.zx2c4.com/wireguard/wgctrl"
)

type Check struct {
	Name   string
	OK     bool
	Detail string
}

func Doctor(ctx context.Context, cfg *Config, container string) []Check {
	checks := []Check{{Name: "configuration", OK: true, Detail: fmt.Sprintf("%s/%s", cfg.Runtime, cfg.Role)}}
	if cfg.Role == "host" {
		for _, name := range cfg.Interfaces {
			_, err := net.InterfaceByName(name)
			checks = append(checks, Check{Name: "LAN interface " + name, OK: err == nil, Detail: errorDetail(err, "available")})
		}
		checks = append(checks, Check{Name: "administrator", OK: isAdministrator(), Detail: "required by tunnel and NAT setup"})
	}
	if cfg.Role == "guest" {
		endpoint, ip, err := ResolveEndpoint(cfg.HostEndpoint)
		detail := errorDetail(err, "")
		if err == nil {
			detail = fmt.Sprintf("%s (%s)", endpoint, ip)
		}
		checks = append(checks, Check{Name: "host endpoint", OK: err == nil, Detail: detail})
	}
	_, helperErr := findHelper()
	if runtime.GOOS == "linux" && commandExists("ip") {
		// The Linux kernel path may work without the helper.
		helperErr = nil
	}
	checks = append(checks, Check{Name: "WireGuard implementation", OK: helperErr == nil, Detail: errorDetail(helperErr, "available")})
	client, err := wgctrl.New()
	if err == nil {
		defer client.Close()
		devices, listErr := client.Devices()
		if listErr == nil {
			for _, device := range devices {
				if strings.HasPrefix(device.Name, "haos") || strings.HasPrefix(device.Name, "utun") {
					detail := "no handshake"
					ok := false
					if len(device.Peers) > 0 && !device.Peers[0].LastHandshakeTime.IsZero() {
						age := time.Since(device.Peers[0].LastHandshakeTime).Round(time.Second)
						detail = fmt.Sprintf("last handshake %s ago", age)
						ok = age < 2*time.Minute
					}
					checks = append(checks, Check{Name: "WireGuard handshake", OK: ok, Detail: detail})
					break
				}
			}
		}
	}
	if container != "" {
		if _, err := exec.LookPath("docker"); err != nil {
			checks = append(checks, Check{Name: "container", OK: false, Detail: "docker command not found"})
		} else {
			inspect := exec.CommandContext(ctx, "docker", "inspect", "-f", "{{.State.Running}}", container)
			out, inspectErr := inspect.Output()
			running := inspectErr == nil && strings.TrimSpace(string(out)) == "true"
			checks = append(checks, Check{Name: "container " + container, OK: running, Detail: errorDetail(inspectErr, strings.TrimSpace(string(out)))})
			if running {
				resolve := exec.CommandContext(ctx, "docker", "exec", container, "getent", "hosts", DefaultHostEndpoint)
				out, resolveErr := resolve.CombinedOutput()
				checks = append(checks, Check{Name: "container host endpoint DNS", OK: resolveErr == nil, Detail: strings.TrimSpace(string(out))})
			}
		}
	}
	if cfg.StateFile != "" {
		state, err := LoadState(cfg.StateFile)
		checks = append(checks, Check{Name: "managed network state", OK: err == nil, Detail: errorDetail(err, cfg.StateFile)})
		if err == nil {
			live := !state.LastGuestHeartbeat.IsZero() && time.Since(state.LastGuestHeartbeat) < 15*time.Second
			detail := "no guest heartbeat"
			if !state.LastGuestHeartbeat.IsZero() {
				detail = fmt.Sprintf("last heartbeat %s ago", time.Since(state.LastGuestHeartbeat).Round(time.Second))
			}
			checks = append(checks, Check{Name: "discovery relay heartbeat", OK: live, Detail: detail})
			checks = append(checks, Check{Name: "discovery relay counters", OK: true, Detail: fmt.Sprintf("sent=%d received=%d mdns=%d ssdp=%d duplicates=%d", state.RelaySent, state.RelayReceived, state.MDNSRelayed, state.SSDPRelayed, state.DuplicatesDropped)})
		}
	}
	return checks
}

func Cleanup(ctx context.Context, cfg *Config, purge bool, configPaths ...string) error {
	if cfg.StateFile != "" {
		state, err := LoadState(cfg.StateFile)
		if err == nil {
			if err := cleanupHost(ctx, state); err != nil {
				return err
			}
			if state.Interface != "" {
				_ = removeTunnelInterface(ctx, state.Interface)
			}
			_ = stopOwnedHelper(ctx, state)
			_ = os.Remove(cfg.StateFile)
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	if purge {
		for _, path := range configPaths {
			if path != "" {
				_ = os.Remove(path)
			}
		}
	}
	return nil
}

func errorDetail(err error, success string) string {
	if err != nil {
		return err.Error()
	}
	return success
}
