package netagent

import (
	"context"
	"io"
	"net"
	"testing"
	"time"
)

func TestHTTPProxyForwardsStreams(t *testing.T) {
	backend, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	go func() {
		connection, acceptErr := backend.Accept()
		if acceptErr != nil {
			return
		}
		defer connection.Close()
		_, _ = io.Copy(connection, connection)
	}()
	ctx, cancel := context.WithCancel(context.Background())
	proxy, err := startTCPProxy(ctx, "127.0.0.1:0", backend.Addr().String())
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	connection, err := net.DialTimeout("tcp4", proxy.Address(), time.Second)
	if err != nil {
		cancel()
		_ = proxy.Close()
		t.Fatal(err)
	}
	if _, err := connection.Write([]byte("dashboard")); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, len("dashboard"))
	if _, err := io.ReadFull(connection, buffer); err != nil || string(buffer) != "dashboard" {
		t.Fatalf("forwarded payload %q: %v", buffer, err)
	}
	_ = connection.Close()
	cancel()
	if err := proxy.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestHTTPProxyReportsOccupiedPort(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if _, err := startTCPProxy(context.Background(), listener.Addr().String(), "127.0.0.1:1"); err == nil {
		t.Fatal("expected occupied listener error")
	}
}
