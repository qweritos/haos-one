package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDockerProxyRewritesRequestsResponsesAndUpgrade(t *testing.T) {
	root, err := os.MkdirTemp("/tmp", "h1-agent-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	upstreamPath := filepath.Join(root, "docker-real.sock")
	frontendPath := filepath.Join(root, "docker.sock")
	listener, err := net.Listen("unix", upstreamPath)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	server := &http.Server{Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if strings.Contains(request.URL.Path, "/containers/create") {
			body, _ := io.ReadAll(request.Body)
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write(body)
			return
		}
		if request.URL.Path == "/info" {
			writer.Header().Set("Content-Type", "application/json")
			writer.Header().Set("Trailer", "X-Test-Trailer")
			writer.WriteHeader(http.StatusOK)
			_, _ = writer.Write([]byte(`{"Warnings":["existing"]}`))
			return
		}
		if request.URL.Path == "/events" {
			hijacker := writer.(http.Hijacker)
			connection, buffer, _ := hijacker.Hijack()
			defer connection.Close()
			_, _ = buffer.WriteString("HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: tcp\r\n\r\npong")
			_ = buffer.Flush()
			return
		}
		writer.WriteHeader(http.StatusNoContent)
	})}
	go server.Serve(listener)
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	proxy := &DockerProxy{Frontend: frontendPath, Upstream: upstreamPath, InjectUdev: true}
	ready := make(chan struct{})
	go func() { _ = proxy.Run(ctx, func() { close(ready) }) }()
	select {
	case <-ready:
	case <-time.After(3 * time.Second):
		t.Fatal("proxy did not become ready")
	}
	defer func() {
		cancel()
		shutdown, stop := context.WithTimeout(context.Background(), time.Second)
		defer stop()
		_ = proxy.Close(shutdown)
	}()

	client := &http.Client{Transport: &http.Transport{DialContext: func(dialCtx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(dialCtx, "unix", frontendPath)
	}}}
	requestBody := `{"Image":"ghcr.io/home-assistant/amd64-hassio-supervisor:latest","Domainname":"homeassistant","HostConfig":{"Ulimits":[]}}`
	response, err := client.Post("http://docker/v1.47/containers/create?name=hassio_supervisor", "application/json", strings.NewReader(requestBody))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	var create map[string]any
	if err := json.Unmarshal(body, &create); err != nil {
		t.Fatal(err)
	}
	if _, exists := create["Domainname"]; exists {
		t.Error("Domainname was not removed")
	}
	environment, _ := stringSlice(create["Env"])
	if !containsString(environment, "USE_UDEV_SHIM=active") {
		t.Fatalf("udev shim not injected: %#v", environment)
	}

	response, err = client.Get("http://docker/info")
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(response.Body)
	_ = response.Body.Close()
	if !strings.Contains(string(body), compatWarning) {
		t.Fatalf("compat warning missing: %s", body)
	}

	connection, err := net.Dial("unix", frontendPath)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	_, _ = fmt.Fprint(connection, "GET /events HTTP/1.1\r\nHost: docker\r\nConnection: Upgrade\r\nUpgrade: tcp\r\n\r\n")
	reader := bufio.NewReader(connection)
	upgrade, err := http.ReadResponse(reader, &http.Request{Method: http.MethodGet})
	if err != nil {
		t.Fatal(err)
	}
	if upgrade.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("upgrade status=%d", upgrade.StatusCode)
	}
	payload := make([]byte, 4)
	if _, err := io.ReadFull(reader, payload); err != nil {
		t.Fatal(err)
	}
	if string(payload) != "pong" {
		t.Fatalf("upgrade payload=%q", payload)
	}

	if _, err := os.Stat(frontendPath); err != nil {
		t.Fatal(err)
	}
}
