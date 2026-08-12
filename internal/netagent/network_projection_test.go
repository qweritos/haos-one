package netagent

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWriteGuestNetworkProjection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "network.json")
	tunnel := &Tunnel{
		Name: "haoswg0",
		Config: &Config{
			TunnelCIDR:  "10.203.0.0/30",
			Address:     "10.203.0.2",
			PeerAddress: "10.203.0.1",
		},
		BootstrapIPs: []net.IP{net.ParseIP("192.168.65.7"), net.ParseIP("::1")},
	}
	now := time.Unix(1234, 0)
	if err := writeGuestNetworkProjection(path, tunnel, now); err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var projection guestNetworkProjection
	if err := json.Unmarshal(payload, &projection); err != nil {
		t.Fatal(err)
	}
	if projection.Interface != "haoswg0" || projection.Address != "10.203.0.2" || projection.Prefix != 30 || projection.Gateway != "10.203.0.1" || projection.UpdatedUnix != 1234 {
		t.Fatalf("unexpected projection: %+v", projection)
	}
	if len(projection.Nameservers) != 1 || projection.Nameservers[0] != "192.168.65.7" {
		t.Fatalf("unexpected nameservers: %v", projection.Nameservers)
	}
}
