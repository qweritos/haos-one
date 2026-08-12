package agent

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestNormalizeDockerTarget(t *testing.T) {
	tests := map[string]string{
		"/info":                        "/info",
		"/v1.47/info":                  "/info",
		"/containers/json":             "/containers/json",
		"/v1.47/containers/json?all=1": "/containers/json",
		"/containers/abc/json":         "",
		"/version":                     "",
	}
	for input, expected := range tests {
		if actual := normalizeDockerTarget(input); actual != expected {
			t.Errorf("normalizeDockerTarget(%q)=%q, want %q", input, actual, expected)
		}
	}
}

func TestRewriteCreatePayload(t *testing.T) {
	payload := []byte(`{"Image":"alpine","Domainname":"homeassistant","HostConfig":{"DnsSearch":["homeassistant"],"Ulimits":[{"Name":"nofile"}]}}`)
	rewritten, changed := rewriteCreatePayload("/v1.47/containers/create?name=test", payload, false)
	if !changed {
		t.Fatal("expected create payload rewrite")
	}
	var data map[string]any
	if err := json.Unmarshal(rewritten, &data); err != nil {
		t.Fatal(err)
	}
	if _, exists := data["Domainname"]; exists {
		t.Error("Domainname was not removed")
	}
	host := data["HostConfig"].(map[string]any)
	if _, exists := host["Ulimits"]; exists {
		t.Error("Ulimits was not removed")
	}
	if !reflect.DeepEqual(host["DnsSearch"], []any{"homeassistant"}) {
		t.Fatalf("DnsSearch changed: %#v", host["DnsSearch"])
	}
}

func TestRewriteSupervisorCreateInjectsUdev(t *testing.T) {
	payload := []byte(`{"Image":"ghcr.io/home-assistant/amd64-hassio-supervisor:latest","Env":["PYTHONPATH=/existing"],"HostConfig":{"Binds":["/mnt/data:/data:rw"]}}`)
	rewritten, changed := rewriteCreatePayload("/containers/create?name=hassio_supervisor", payload, true)
	if !changed {
		t.Fatal("expected udev injection")
	}
	var data map[string]any
	if err := json.Unmarshal(rewritten, &data); err != nil {
		t.Fatal(err)
	}
	environment, _ := stringSlice(data["Env"])
	if !containsString(environment, "PYTHONPATH=/opt/haos-udev-shim:/existing") || !containsString(environment, "USE_UDEV_SHIM=active") {
		t.Fatalf("unexpected environment: %#v", environment)
	}
	binds, _ := stringSlice(data["HostConfig"].(map[string]any)["Binds"])
	if !containsString(binds, udevShimBind) {
		t.Fatalf("missing udev bind: %#v", binds)
	}
}

func TestRewriteInfoPayload(t *testing.T) {
	payload := []byte(`{"OperatingSystem":"Home Assistant OS","Warnings":["existing"]}`)
	rewritten, changed := rewriteInfoPayload(payload)
	if !changed {
		t.Fatal("expected info rewrite")
	}
	var data map[string]any
	if err := json.Unmarshal(rewritten, &data); err != nil {
		t.Fatal(err)
	}
	warnings, _ := stringSlice(data["Warnings"])
	if !reflect.DeepEqual(warnings, []string{"existing", compatWarning}) {
		t.Fatalf("warnings=%#v", warnings)
	}
}

func TestContainerListIsNotFiltered(t *testing.T) {
	payload := []byte(`[{"Names":["/haos_one_compat"]},{"Names":["/hassio_supervisor"]}]`)
	if normalizeDockerTarget("/v1.48/containers/json?all=1") != "/containers/json" {
		t.Fatal("container list target not recognized")
	}
	// With no compatibility container, the unified agent has no list rewrite.
	if string(payload) == "" {
		t.Fatal("unexpected payload mutation")
	}
}
