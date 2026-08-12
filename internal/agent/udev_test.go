package agent

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestResolveUdevShim(t *testing.T) {
	remapped := []byte("0 100000 65536\n")
	identity := []byte("0 0 4294967295\n")
	for _, test := range []struct {
		mode    string
		mapData []byte
		want    bool
	}{
		{"auto", remapped, true},
		{"auto", identity, false},
		{"force", identity, true},
		{"off", remapped, false},
	} {
		got, err := ResolveUdevShim(test.mode, test.mapData)
		if err != nil || got != test.want {
			t.Fatalf("ResolveUdevShim(%q)=(%v,%v), want %v", test.mode, got, err, test.want)
		}
	}
	if _, err := ResolveUdevShim("invalid", identity); err == nil {
		t.Fatal("invalid mode accepted")
	}
}

func TestMigrateSupervisorUdev(t *testing.T) {
	root, err := os.MkdirTemp("/tmp", "h1-udev-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	socket := filepath.Join(root, "docker.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	var deleted atomic.Bool
	server := &http.Server{Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.Method {
		case http.MethodGet:
			writer.Header().Set("Content-Type", "application/json")
			fmt.Fprint(writer, `{"Config":{"Env":[]},"Mounts":[]}`)
		case http.MethodDelete:
			deleted.Store(true)
			writer.WriteHeader(http.StatusNoContent)
		}
	})}
	go server.Serve(listener)
	defer server.Close()
	migrated, err := MigrateSupervisorUdev(context.Background(), socket, true)
	if err != nil {
		t.Fatal(err)
	}
	if !migrated || !deleted.Load() {
		t.Fatal("Supervisor was not migrated")
	}
}
