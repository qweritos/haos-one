//go:build windows

package netagent

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
)

const windowsFirewallRule = "HAOS One WireGuard"

func prepareHost(ctx context.Context, cfg *Config, tunnel *Tunnel) (*State, error) {
	state := &State{Version: ConfigVersion, Platform: "windows", Interface: tunnel.Name, WindowsFirewallRule: windowsFirewallRule}
	natName := "haos-one"
	interfaceAliases := make([]string, 0, len(cfg.Interfaces))
	for _, name := range cfg.Interfaces {
		interfaceAliases = append(interfaceAliases, "'"+psQuote(name)+"'")
	}
	interfaceArray := "@(" + strings.Join(interfaceAliases, ",") + ")"
	executable, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("resolve helper executable: %w", err)
	}
	ps := fmt.Sprintf(`
function Convert-IPv4ToUInt32([string]$Address) {
  $bytes = [System.Net.IPAddress]::Parse($Address).GetAddressBytes()
  [Array]::Reverse($bytes)
  return [BitConverter]::ToUInt32($bytes, 0)
}
function Test-IPv4Prefix([string]$Address, [string]$Prefix) {
  if ([string]::IsNullOrWhiteSpace($Prefix)) { return $false }
  $parts = $Prefix.Split('/')
  if ($parts.Count -ne 2) { return $false }
  $bits = 0
  if (-not [int]::TryParse($parts[1], [ref]$bits) -or $bits -lt 0 -or $bits -gt 32) { return $false }
  $network = $null
  if (-not [System.Net.IPAddress]::TryParse($parts[0], [ref]$network) -or $network.AddressFamily -ne [System.Net.Sockets.AddressFamily]::InterNetwork) { return $false }
  $mask = if ($bits -eq 0) { [uint32]0 } else { [uint32]::MaxValue -shl (32 - $bits) }
  return ((Convert-IPv4ToUInt32 $Address) -band $mask) -eq ((Convert-IPv4ToUInt32 $parts[0]) -band $mask)
}
$created = $false
try {
  $existing = Get-NetNat -ErrorAction SilentlyContinue
  $matching = $existing | Where-Object { Test-IPv4Prefix '%s' $_.InternalIPInterfaceAddressPrefix } | Select-Object -First 1
  if (-not $matching) {
    if ($existing) { throw 'An existing WinNAT does not include the selected tunnel CIDR; rerun init with a free /30 inside its prefix' }
    New-NetNat -Name '%s' -InternalIPInterfaceAddressPrefix '%s' -ErrorAction Stop | Out-Null
    $created = $true
    Write-Output CREATED
  } else { Write-Output ('REUSED:' + $matching.Name) }
  Set-NetIPInterface -InterfaceAlias '%s' -Forwarding Enabled -ErrorAction Stop
  New-NetFirewallRule -DisplayName '%s' -Direction Inbound -Action Allow -Protocol UDP -LocalPort %d -Profile Any -ErrorAction Stop | Out-Null
  New-NetFirewallRule -DisplayName '%s' -Direction Inbound -Action Allow -Protocol TCP -LocalPort %d -Profile Any -InterfaceAlias %s -ErrorAction Stop | Out-Null
  New-NetFirewallRule -DisplayName '%s' -Direction Inbound -Action Allow -Protocol UDP -Profile Private -InterfaceAlias %s -Program '%s' -ErrorAction Stop | Out-Null
} catch {
  Remove-NetFirewallRule -DisplayName '%s' -ErrorAction SilentlyContinue
  Set-NetIPInterface -InterfaceAlias '%s' -Forwarding Disabled -ErrorAction SilentlyContinue
  if ($created) { Remove-NetNat -Name '%s' -Confirm:$false -ErrorAction SilentlyContinue }
  throw
}
`, psQuote(cfg.Address), psQuote(natName), psQuote(cfg.TunnelCIDR), psQuote(tunnel.Name), psQuote(windowsFirewallRule), cfg.ListenPort, psQuote(windowsFirewallRule), cfg.EffectiveHTTPPort(), interfaceArray, psQuote(windowsFirewallRule), interfaceArray, psQuote(executable), psQuote(windowsFirewallRule), psQuote(tunnel.Name), psQuote(natName))
	out, err := commandOutput(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", ps)
	if err != nil {
		return nil, err
	}
	if strings.Contains(out, "CREATED") {
		state.WindowsNAT = natName
		state.WindowsNATCreated = true
	} else if i := strings.Index(out, "REUSED:"); i >= 0 {
		state.WindowsNAT = strings.TrimSpace(out[i+7:])
	}
	if err := SaveState(cfg.StateFile, state); err != nil {
		_ = cleanupHost(context.Background(), state)
		return nil, err
	}
	return state, nil
}

func cleanupHost(ctx context.Context, state *State) error {
	ps := fmt.Sprintf("Remove-NetFirewallRule -DisplayName '%s' -ErrorAction SilentlyContinue; ", psQuote(state.WindowsFirewallRule))
	if state.WindowsNATCreated && state.WindowsNAT != "" {
		ps += fmt.Sprintf("Remove-NetNat -Name '%s' -Confirm:$false -ErrorAction SilentlyContinue; ", psQuote(state.WindowsNAT))
	}
	if state.Interface != "" {
		ps += fmt.Sprintf("Set-NetIPInterface -InterfaceAlias '%s' -Forwarding Disabled -ErrorAction SilentlyContinue", psQuote(state.Interface))
	}
	ps += "; exit 0"
	return runCommand(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", ps)
}

func addGuestRoutes(ctx context.Context, tunnel string, cidrs []string) error {
	return fmt.Errorf("guest routes are unsupported on Windows")
}

func removeGuestRoutes(ctx context.Context, tunnel string, cidrs []string) error { return nil }

func injectUDP(source net.IP, sourcePort int, destination net.IP, destinationPort int, ttl int, payload []byte, interfaceName string) error {
	return fmt.Errorf("packet injection is only supported by the Linux guest")
}

func isAdministrator() bool {
	out, err := commandOutput(context.Background(), "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", "([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)")
	return err == nil && strings.EqualFold(strings.TrimSpace(out), "true")
}

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func stopOwnedHelper(ctx context.Context, state *State) error {
	if state.HelperPID <= 0 {
		return nil
	}
	ps := fmt.Sprintf("$p=Get-CimInstance Win32_Process -Filter 'ProcessId=%d' -ErrorAction SilentlyContinue; if ($p -and $p.CommandLine -like '*wireguard-go*') { Stop-Process -Id %d -Force }", state.HelperPID, state.HelperPID)
	return runCommand(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", ps)
}

func psQuote(value string) string { return strings.ReplaceAll(value, "'", "''") }
