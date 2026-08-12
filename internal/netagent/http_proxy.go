package netagent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

type HTTPProxy struct {
	listener net.Listener
	target   string
	wg       sync.WaitGroup
}

func StartHTTPProxy(ctx context.Context, cfg *Config) (*HTTPProxy, error) {
	port := cfg.EffectiveHTTPPort()
	targetPort := cfg.EffectiveGuestHTTPPort()
	proxy, err := startTCPProxy(ctx, fmt.Sprintf(":%d", port), net.JoinHostPort(cfg.PeerAddress, fmt.Sprint(targetPort)))
	if err != nil {
		return nil, fmt.Errorf("listen for Home Assistant HTTP on TCP %d: %w", port, err)
	}
	return proxy, nil
}

func startTCPProxy(ctx context.Context, listenAddress, target string) (*HTTPProxy, error) {
	listener, err := net.Listen("tcp4", listenAddress)
	if err != nil {
		return nil, err
	}
	proxy := &HTTPProxy{listener: listener, target: target}
	proxy.wg.Add(1)
	go proxy.serve(ctx)
	return proxy, nil
}

func (p *HTTPProxy) Address() string { return p.listener.Addr().String() }

func (p *HTTPProxy) Close() error {
	err := p.listener.Close()
	p.wg.Wait()
	if errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}

func (p *HTTPProxy) serve(ctx context.Context) {
	defer p.wg.Done()
	go func() {
		<-ctx.Done()
		_ = p.listener.Close()
	}()
	for {
		client, err := p.listener.Accept()
		if err != nil {
			return
		}
		p.wg.Add(1)
		go func() {
			defer p.wg.Done()
			p.forward(ctx, client)
		}()
	}
}

func (p *HTTPProxy) forward(ctx context.Context, client net.Conn) {
	defer client.Close()
	dialer := net.Dialer{Timeout: 10 * time.Second}
	guest, err := dialer.DialContext(ctx, "tcp4", p.target)
	if err != nil {
		return
	}
	defer guest.Close()
	done := make(chan struct{}, 2)
	copyStream := func(destination, source net.Conn) {
		_, _ = io.Copy(destination, source)
		if tcp, ok := destination.(*net.TCPConn); ok {
			_ = tcp.CloseWrite()
		}
		done <- struct{}{}
	}
	go copyStream(guest, client)
	go copyStream(client, guest)
	select {
	case <-ctx.Done():
	case <-done:
	}
}
