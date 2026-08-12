package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadNetworkProfileUsesFreshProjection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "network.json")
	now := time.Unix(1_700_000_000, 0)
	payload, _ := json.Marshal(projectionFile{Version: 1, Interface: "haoswg0", Address: "10.203.0.2", Prefix: 30, Gateway: "10.203.0.1", Nameservers: []string{"192.168.5.1"}, UpdatedUnix: now.Unix()})
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	profile := loadNetworkProfile(path, now)
	if profile.Interface != "haoswg0" || profile.Address != "10.203.0.2" || profile.Gateway != "10.203.0.1" || profile.Prefix != 30 {
		t.Fatalf("unexpected profile: %#v", profile)
	}
	settings := connectionSettings(profile)
	if settings["connection"]["type"].Value() != "802-3-ethernet" {
		t.Fatalf("projection is not fake Ethernet: %#v", settings)
	}
}

func TestLoadNetworkProfileRejectsStaleProjection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "network.json")
	now := time.Unix(1_700_000_000, 0)
	payload, _ := json.Marshal(projectionFile{Version: 1, Interface: "haoswg0", Address: "10.203.0.2", Prefix: 30, Gateway: "10.203.0.1", UpdatedUnix: now.Add(-16 * time.Second).Unix()})
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	profile := loadNetworkProfile(path, now)
	if profile.Interface != "eth0" {
		t.Fatalf("stale projection remained active: %#v", profile)
	}
}

func TestNetworkManagerObjectSurface(t *testing.T) {
	objects := networkManagerObjects(func() networkProfile {
		return networkProfile{Interface: "eth0", Address: "192.168.1.100", Prefix: 24, Gateway: "192.168.1.1", Nameservers: []string{"192.168.1.1"}}
	})
	if len(objects) != 8 {
		t.Fatalf("got %d D-Bus objects", len(objects))
	}
	root := objects[0].source()
	if root["Connectivity"].Value() != uint32(4) || root["PrimaryConnection"].Value() != nmActivePath {
		t.Fatalf("unexpected root properties: %#v", root)
	}
}
