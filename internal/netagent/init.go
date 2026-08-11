package netagent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

type InitOptions struct {
	Runtime      string
	OutputDir    string
	HostEndpoint string
	ListenPort   int
	TunnelCIDR   string
	Interfaces   []string
	LANCIDRs     []string
	Force        bool
}

type InitResult struct {
	HostPath  string
	GuestPath string
	Host      *Config
	Guest     *Config
}

func Init(opts InitOptions) (*InitResult, error) {
	if opts.Runtime == "" {
		opts.Runtime = "auto"
	}
	runtimeName, err := DetectRuntime(opts.Runtime)
	if err != nil {
		return nil, err
	}
	if opts.OutputDir == "" {
		opts.OutputDir, err = DefaultConfigDir()
		if err != nil {
			return nil, err
		}
	}
	hostPath := filepath.Join(opts.OutputDir, "host.yaml")
	guestPath := filepath.Join(opts.OutputDir, "guest.yaml")
	if !opts.Force {
		for _, path := range []string{hostPath, guestPath} {
			if _, statErr := os.Stat(path); statErr == nil {
				return nil, fmt.Errorf("%s already exists; use --force to rotate keys", path)
			} else if !os.IsNotExist(statErr) {
				return nil, statErr
			}
		}
	}
	if opts.HostEndpoint == "" {
		opts.HostEndpoint = DefaultHostEndpoint
	}
	if opts.ListenPort == 0 {
		opts.ListenPort = DefaultListenPort
	}
	autoInterface := len(opts.Interfaces) == 0
	autoCIDRs := len(opts.LANCIDRs) == 0
	if autoInterface {
		iface, err := DefaultInterface()
		if err != nil {
			return nil, fmt.Errorf("detect LAN interface: %w", err)
		}
		opts.Interfaces = []string{iface}
	}
	if autoCIDRs {
		for _, name := range opts.Interfaces {
			cidrs, err := InterfaceCIDRs(name)
			if err != nil {
				return nil, err
			}
			opts.LANCIDRs = append(opts.LANCIDRs, cidrs...)
		}
	}
	opts.LANCIDRs = uniqueStrings(opts.LANCIDRs)
	if len(opts.LANCIDRs) == 0 {
		return nil, errors.New("no IPv4 LAN CIDRs found; use --lan-cidr")
	}
	dockerCIDRs := dockerNetworkCIDRs()
	if left, right, overlap := firstCIDROverlap(opts.LANCIDRs, dockerCIDRs); overlap {
		return nil, fmt.Errorf("LAN CIDR %s overlaps Docker network %s; move the Docker network before enabling host-assisted routing", left, right)
	}
	if opts.TunnelCIDR == "" || opts.TunnelCIDR == "auto" {
		conflicts := append([]string(nil), opts.LANCIDRs...)
		conflicts = append(conflicts, dockerCIDRs...)
		opts.TunnelCIDR = ""
		if runtime.GOOS == "windows" {
			opts.TunnelCIDR, err = chooseWindowsNATCIDR(conflicts)
		}
		if opts.TunnelCIDR == "" && err == nil {
			opts.TunnelCIDR, err = ChooseTunnelCIDR(conflicts)
		}
		if err != nil {
			return nil, err
		}
	}
	if tunnel, conflict, overlap := firstCIDROverlap([]string{opts.TunnelCIDR}, append(append([]string(nil), opts.LANCIDRs...), dockerCIDRs...)); overlap {
		return nil, fmt.Errorf("tunnel CIDR %s overlaps existing network %s", tunnel, conflict)
	}
	hostIP, guestIP, err := TunnelAddresses(opts.TunnelCIDR)
	if err != nil {
		return nil, err
	}
	hostKey, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		return nil, err
	}
	guestKey, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		return nil, err
	}
	stateFile := DefaultStateFile(opts.OutputDir)
	host := &Config{
		Version: ConfigVersion, Role: "host", Runtime: runtimeName,
		PrivateKey: hostKey.String(), PeerPublicKey: guestKey.PublicKey().String(),
		TunnelCIDR: opts.TunnelCIDR, Address: hostIP.String(), PeerAddress: guestIP.String(),
		ListenPort: opts.ListenPort, RelayPort: DefaultRelayPort, MTU: DefaultMTU,
		AutoInterface: autoInterface, AutoCIDRs: autoCIDRs,
		Interfaces: opts.Interfaces, LANCIDRs: opts.LANCIDRs, StateFile: stateFile,
	}
	guest := &Config{
		Version: ConfigVersion, Role: "guest", Runtime: runtimeName,
		PrivateKey: guestKey.String(), PeerPublicKey: hostKey.PublicKey().String(),
		TunnelCIDR: opts.TunnelCIDR, Address: guestIP.String(), PeerAddress: hostIP.String(),
		RelayPort: DefaultRelayPort, MTU: DefaultMTU,
		HostEndpoint: net.JoinHostPort(opts.HostEndpoint, strconv.Itoa(opts.ListenPort)),
		LANCIDRs:     opts.LANCIDRs,
	}
	if err := SaveConfig(hostPath, host); err != nil {
		return nil, err
	}
	if err := SaveConfig(guestPath, guest); err != nil {
		return nil, err
	}
	return &InitResult{HostPath: hostPath, GuestPath: guestPath, Host: host, Guest: guest}, nil
}

func firstCIDROverlap(left, right []string) (string, string, bool) {
	for _, leftValue := range left {
		_, leftNet, leftErr := net.ParseCIDR(leftValue)
		if leftErr != nil {
			continue
		}
		for _, rightValue := range right {
			_, rightNet, rightErr := net.ParseCIDR(rightValue)
			if rightErr == nil && (leftNet.Contains(rightNet.IP) || rightNet.Contains(leftNet.IP)) {
				return leftValue, rightValue, true
			}
		}
	}
	return "", "", false
}

func dockerNetworkCIDRs() []string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	idsOutput, err := exec.CommandContext(ctx, "docker", "network", "ls", "-q").Output()
	if err != nil {
		return nil
	}
	ids := strings.Fields(string(idsOutput))
	if len(ids) == 0 {
		return nil
	}
	args := append([]string{"network", "inspect"}, ids...)
	out, err := exec.CommandContext(ctx, "docker", args...).Output()
	if err != nil {
		return nil
	}
	var networks []struct {
		IPAM struct {
			Config []struct {
				Subnet string `json:"Subnet"`
			} `json:"Config"`
		} `json:"IPAM"`
	}
	if json.Unmarshal(out, &networks) != nil {
		return nil
	}
	var result []string
	for _, network := range networks {
		for _, ipam := range network.IPAM.Config {
			if ip, _, parseErr := net.ParseCIDR(ipam.Subnet); parseErr == nil && ip.To4() != nil {
				result = append(result, ipam.Subnet)
			}
		}
	}
	return uniqueStrings(result)
}

func chooseWindowsNATCIDR(used []string) (string, error) {
	ps := "@(Get-NetNat -ErrorAction SilentlyContinue | Select-Object -ExpandProperty InternalIPInterfaceAddressPrefix) -join \"`n\""
	out, err := commandOutput(context.Background(), "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", ps)
	if err != nil || strings.TrimSpace(out) == "" {
		return "", nil
	}
	prefixes := uniqueStrings(strings.Split(out, "\n"))
	if len(prefixes) > 1 {
		return "", fmt.Errorf("multiple WinNAT prefixes found (%s); select --tunnel-cidr explicitly", strings.Join(prefixes, ", "))
	}
	return ChooseTunnelCIDRWithin(prefixes[0], used)
}

func ChooseTunnelCIDRWithin(prefix string, used []string) (string, error) {
	baseIP, parent, err := net.ParseCIDR(prefix)
	if err != nil || baseIP.To4() == nil {
		return "", fmt.Errorf("invalid WinNAT IPv4 prefix %q", prefix)
	}
	ones, bits := parent.Mask.Size()
	if bits != 32 || ones > 30 {
		return "", fmt.Errorf("WinNAT prefix %s cannot contain a /30", prefix)
	}
	var occupied []*net.IPNet
	for _, value := range used {
		_, network, parseErr := net.ParseCIDR(value)
		if parseErr == nil {
			occupied = append(occupied, network)
		}
	}
	ifaces, _ := net.Interfaces()
	for _, iface := range ifaces {
		addresses, _ := iface.Addrs()
		for _, address := range addresses {
			ip, _, parseErr := net.ParseCIDR(address.String())
			if parseErr == nil && ip.To4() != nil {
				occupied = append(occupied, &net.IPNet{IP: ip.To4(), Mask: net.CIDRMask(32, 32)})
			}
		}
	}
	base := binaryIPv4(parent.IP)
	size := uint64(1) << uint(32-ones)
	for offset := uint64(0); offset+4 <= size; offset += 4 {
		ip := uint32IPv4(base + uint32(offset))
		if !parent.Contains(ip) {
			break
		}
		candidate := &net.IPNet{IP: ip, Mask: net.CIDRMask(30, 32)}
		conflict := false
		for _, network := range occupied {
			if network.Contains(candidate.IP) || candidate.Contains(network.IP) {
				conflict = true
				break
			}
		}
		if !conflict {
			return candidate.String(), nil
		}
	}
	return "", fmt.Errorf("no unused /30 is available inside WinNAT prefix %s", prefix)
}

func DetectRuntime(requested string) (string, error) {
	switch requested {
	case "docker-desktop", "colima":
		return requested, nil
	case "auto":
	default:
		return "", fmt.Errorf("runtime must be auto, docker-desktop, or colima")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "context", "inspect")
	out, err := cmd.Output()
	if err != nil {
		return "", errors.New("cannot inspect active Docker context; start Docker Desktop or Colima, or pass --runtime")
	}
	lower := strings.ToLower(string(out))
	if strings.Contains(lower, ".colima") || strings.Contains(lower, "colima") {
		if runtime.GOOS != "darwin" {
			return "", errors.New("Colima support is currently macOS-only")
		}
		return "colima", nil
	}
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		return "docker-desktop", nil
	}
	return "", errors.New("the active Docker context is not Docker Desktop or Colima")
}

func DefaultInterface() (string, error) {
	switch runtime.GOOS {
	case "darwin":
		out, err := exec.Command("route", "-n", "get", "default").Output()
		if err != nil {
			return "", err
		}
		s := bufio.NewScanner(bytes.NewReader(out))
		for s.Scan() {
			fields := strings.Fields(s.Text())
			if len(fields) == 2 && fields[0] == "interface:" {
				return fields[1], nil
			}
		}
	case "windows":
		ps := "(Get-NetRoute -DestinationPrefix '0.0.0.0/0' | Sort-Object RouteMetric | Select-Object -First 1).InterfaceAlias"
		out, err := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", ps).Output()
		if err != nil {
			return "", err
		}
		if name := strings.TrimSpace(string(out)); name != "" {
			return name, nil
		}
	default:
		f, err := os.Open("/proc/net/route")
		if err == nil {
			defer f.Close()
			s := bufio.NewScanner(f)
			for s.Scan() {
				fields := strings.Fields(s.Text())
				if len(fields) > 2 && fields[1] == "00000000" {
					return fields[0], nil
				}
			}
		}
	}
	return "", errors.New("default route interface not found")
}

func InterfaceCIDRs(name string) ([]string, error) {
	iface, err := net.InterfaceByName(name)
	if err != nil {
		return nil, fmt.Errorf("interface %s: %w", name, err)
	}
	addrs, err := iface.Addrs()
	if err != nil {
		return nil, err
	}
	var result []string
	for _, addr := range addrs {
		ip, network, err := net.ParseCIDR(addr.String())
		if err == nil && ip.To4() != nil && !ip.IsLoopback() {
			result = append(result, network.String())
		}
	}
	return uniqueStrings(result), nil
}

func ChooseTunnelCIDR(used []string) (string, error) {
	var networks []*net.IPNet
	ifaces, _ := net.Interfaces()
	for _, iface := range ifaces {
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			_, n, err := net.ParseCIDR(addr.String())
			if err == nil && n.IP.To4() != nil {
				networks = append(networks, n)
			}
		}
	}
	for _, value := range used {
		_, n, err := net.ParseCIDR(value)
		if err == nil {
			networks = append(networks, n)
		}
	}
	for third := 0; third < 256; third++ {
		for fourth := 0; fourth < 256; fourth += 4 {
			candidate := fmt.Sprintf("10.203.%d.%d/30", third, fourth)
			ip, n, _ := net.ParseCIDR(candidate)
			conflict := false
			for _, usedNet := range networks {
				if usedNet.Contains(ip) || n.Contains(usedNet.IP) {
					conflict = true
					break
				}
			}
			if !conflict {
				return candidate, nil
			}
		}
	}
	return "", errors.New("no free tunnel /30 found in 10.203.0.0/16")
}

func TunnelAddresses(cidr string) (net.IP, net.IP, error) {
	ip, network, err := net.ParseCIDR(cidr)
	if err != nil || ip.To4() == nil {
		return nil, nil, fmt.Errorf("invalid IPv4 tunnel CIDR %q", cidr)
	}
	ones, bits := network.Mask.Size()
	if bits != 32 || ones != 30 {
		return nil, nil, errors.New("tunnel CIDR must be an IPv4 /30")
	}
	base := binaryIPv4(network.IP)
	return uint32IPv4(base + 1), uint32IPv4(base + 2), nil
}

func binaryIPv4(ip net.IP) uint32 {
	v := ip.To4()
	return uint32(v[0])<<24 | uint32(v[1])<<16 | uint32(v[2])<<8 | uint32(v[3])
}

func uint32IPv4(v uint32) net.IP {
	return net.IPv4(byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}

func uniqueStrings(in []string) []string {
	seen := make(map[string]bool)
	out := make([]string, 0, len(in))
	for _, value := range in {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}

func DockerSnippets(result *InitResult) string {
	guest := result.GuestPath
	mount := guest + ":/etc/haos-one/desktop-network.yaml:ro"
	hostCommand := "sudo haos-one-net host run --config " + shellQuote(result.HostPath)
	continuation := "\\"
	if runtime.GOOS == "windows" {
		hostCommand = ".\\haos-one-net.exe host run --config " + shellQuote(result.HostPath)
		continuation = "`"
	}
	return fmt.Sprintf(`Host agent (run as root/Administrator):
  %s

Docker Run:
  docker run --name haos -ti --privileged -p 8123:8123 %s
    -e USE_DESKTOP_NETWORK=1 %s
    -v %s %s
    -v haos-data:/mnt/data qweritos/haos-one

Compose service fields:
  privileged: true
  environment:
    USE_DESKTOP_NETWORK: "1"
  ports:
    - "8123:8123"
  volumes:
    - %s
    - haos-data:/mnt/data
`, hostCommand, continuation, continuation, shellQuote(mount), continuation, strconv.Quote(mount))
}

func shellQuote(value string) string {
	if runtime.GOOS == "windows" {
		return "'" + strings.ReplaceAll(value, "'", "''") + "'"
	}
	return strconv.Quote(value)
}
