package netagent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

func TestDockerSnippetsActivateDesktopNetworkingFromMount(t *testing.T) {
	result := &InitResult{HostPath: "/tmp/host.yaml", GuestPath: "/tmp/guest.yaml"}
	snippets := DockerSnippets(result)
	if strings.Contains(snippets, "USE_DESKTOP_") {
		t.Fatal("generated snippets must not require a desktop-network activation environment variable")
	}
	if !strings.Contains(snippets, "/tmp/guest.yaml:/etc/haos-one/desktop-network.yaml:ro") {
		t.Fatal("generated snippets do not contain the read-only guest configuration mount")
	}
	if strings.Contains(snippets, "--network host") || strings.Contains(snippets, "-p 8123:8123") || strings.Contains(snippets, `"8123:8123"`) || strings.Contains(snippets, "ports:") {
		t.Fatal("generated snippets must leave LAN and HTTP ingress to the host helper")
	}
}

func TestResolveConfigPathPrecedence(t *testing.T) {
	t.Setenv(ConfigPathEnv, "/tmp/from-env.yaml")
	path, err := ResolveConfigPath("", "host")
	if err != nil || path != "/tmp/from-env.yaml" {
		t.Fatalf("environment path: %q %v", path, err)
	}
	path, err = ResolveConfigPath("/tmp/from-flag.yaml", "host")
	if err != nil || path != "/tmp/from-flag.yaml" {
		t.Fatalf("explicit path: %q %v", path, err)
	}
}

func TestNormalizeDNSName(t *testing.T) {
	name, err := NormalizeDNSName("HomeAssistant.Local.")
	if err != nil || name != DefaultDNSName {
		t.Fatalf("normalized name: %q %v", name, err)
	}
	for _, value := range []string{"homeassistant", "-bad.local", "bad_name.local"} {
		if _, err := NormalizeDNSName(value); err == nil {
			t.Fatalf("expected invalid DNS name %q", value)
		}
	}
}

func TestDefaultExternalRoutesUseSplitDefault(t *testing.T) {
	routes := DefaultExternalRoutes()
	if len(routes) != 2 || routes[0] != "0.0.0.0/1" || routes[1] != "128.0.0.0/1" {
		t.Fatalf("unexpected external routes: %v", routes)
	}
}

func TestResolverIPv4s(t *testing.T) {
	got := resolverIPv4s("nameserver 192.168.65.7\nnameserver 127.0.0.11\nnameserver 192.168.65.7\nnameserver ::1\n")
	if len(got) != 2 || got[0].String() != "192.168.65.7" || got[1].String() != "127.0.0.11" {
		t.Fatalf("unexpected resolver addresses: %v", got)
	}
}

func TestTunnelAddresses(t *testing.T) {
	host, guest, err := TunnelAddresses("10.203.1.4/30")
	if err != nil {
		t.Fatal(err)
	}
	if host.String() != "10.203.1.5" || guest.String() != "10.203.1.6" {
		t.Fatalf("unexpected addresses %s %s", host, guest)
	}
	if _, _, err := TunnelAddresses("10.203.1.0/24"); err == nil {
		t.Fatal("expected prefix error")
	}
}

func TestConfigValidation(t *testing.T) {
	privateKey, peerKey := testKeys(t)
	cfg := Config{Version: 1, Role: "guest", Runtime: "colima", PrivateKey: privateKey, PeerPublicKey: peerKey, TunnelCIDR: "10.203.0.0/30", Address: "10.203.0.2", PeerAddress: "10.203.0.1", RelayPort: 47821, MTU: 1280, HostEndpoint: "host.docker.internal:51821", LANCIDRs: []string{"192.168.1.0/24"}}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
}

func testKeys(t *testing.T) (string, string) {
	t.Helper()
	first, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	second, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	return first.String(), second.PublicKey().String()
}

func TestChooseTunnelCIDRWithin(t *testing.T) {
	got, err := ChooseTunnelCIDRWithin("172.28.0.0/24", []string{"172.28.0.0/30", "172.28.0.4/30"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "172.28.0.8/30" {
		t.Fatalf("got %s", got)
	}
}

func TestCIDROverlap(t *testing.T) {
	left, right, ok := firstCIDROverlap([]string{"192.168.1.0/24"}, []string{"172.17.0.0/16", "192.168.1.128/25"})
	if !ok || left != "192.168.1.0/24" || right != "192.168.1.128/25" {
		t.Fatalf("unexpected overlap result: %q %q %v", left, right, ok)
	}
	if _, _, ok := firstCIDROverlap([]string{"192.168.1.0/24"}, []string{"172.17.0.0/16"}); ok {
		t.Fatal("unexpected overlap")
	}
}

func TestInitDoesNotOverwriteKeysWithoutForce(t *testing.T) {
	dir := t.TempDir()
	opts := InitOptions{Runtime: "colima", OutputDir: dir, Interfaces: []string{"test0"}, LANCIDRs: []string{"192.0.2.0/24"}}
	first, err := Init(opts)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(first.HostPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Init(opts); err == nil {
		t.Fatal("expected existing configuration error")
	}
	after, err := os.ReadFile(filepath.Join(dir, "host.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("existing key material changed")
	}
	opts.Force = true
	if _, err := Init(opts); err != nil {
		t.Fatal(err)
	}
}
