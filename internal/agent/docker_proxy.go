package agent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type DockerProxy struct {
	Frontend   string
	Upstream   string
	InjectUdev bool
	listener   net.Listener
	server     *http.Server
}

func (proxy *DockerProxy) Run(ctx context.Context, ready func()) error {
	if err := waitForUnixSocket(ctx, proxy.Upstream); err != nil {
		return err
	}
	if err := os.Remove(proxy.Frontend); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove stale Docker socket: %w", err)
	}
	listener, err := net.Listen("unix", proxy.Frontend)
	if err != nil {
		return fmt.Errorf("listen on Docker socket %s: %w", proxy.Frontend, err)
	}
	proxy.listener = listener
	defer func() {
		_ = listener.Close()
		_ = os.Remove(proxy.Frontend)
	}()
	if err := mirrorSocketPermissions(proxy.Upstream, proxy.Frontend); err != nil {
		return err
	}

	transport := &http.Transport{
		DialContext: func(dialCtx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(dialCtx, "unix", proxy.Upstream)
		},
		DisableCompression: true,
	}
	reverse := &httputil.ReverseProxy{
		Rewrite: func(request *httputil.ProxyRequest) {
			request.SetURL(&urlForDockerProxy)
			request.Out.Host = "docker"
		},
		Transport:      transport,
		FlushInterval:  -1,
		ModifyResponse: proxy.modifyResponse,
		ErrorHandler: func(writer http.ResponseWriter, _ *http.Request, proxyErr error) {
			log.Printf("haos-one-agent: Docker proxy: %v", proxyErr)
			http.Error(writer, "Docker API unavailable", http.StatusBadGateway)
		},
	}
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if isContainerCreate(request.URL.RequestURI()) {
			body, readErr := io.ReadAll(request.Body)
			if readErr != nil {
				http.Error(writer, readErr.Error(), http.StatusBadRequest)
				return
			}
			_ = request.Body.Close()
			if rewritten, changed := rewriteCreatePayload(request.URL.RequestURI(), body, proxy.InjectUdev); changed {
				body = rewritten
			}
			request.Body = io.NopCloser(bytes.NewReader(body))
			request.ContentLength = int64(len(body))
			request.Header.Del("Transfer-Encoding")
			request.TransferEncoding = nil
			request.Header.Set("Content-Length", strconv.Itoa(len(body)))
		}
		reverse.ServeHTTP(writer, request)
	})
	proxy.server = &http.Server{Handler: handler, ReadHeaderTimeout: 30 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = proxy.server.Shutdown(shutdown)
	}()
	if ready != nil {
		ready()
	}
	err = proxy.server.Serve(listener)
	transport.CloseIdleConnections()
	if errors.Is(err, http.ErrServerClosed) || ctx.Err() != nil {
		return nil
	}
	return err
}

var urlForDockerProxy = url.URL{Scheme: "http", Host: "docker"}

func (proxy *DockerProxy) modifyResponse(response *http.Response) error {
	if normalizeDockerTarget(response.Request.URL.RequestURI()) != "/info" || response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil
	}
	if !strings.Contains(strings.ToLower(response.Header.Get("Content-Type")), "json") {
		return nil
	}
	encoding := response.Header.Get("Content-Encoding")
	if encoding != "" && !strings.EqualFold(encoding, "identity") {
		return nil
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return err
	}
	_ = response.Body.Close()
	rewritten, changed := rewriteInfoPayload(body)
	if !changed {
		response.Body = io.NopCloser(bytes.NewReader(body))
		return nil
	}
	response.Body = io.NopCloser(bytes.NewReader(rewritten))
	response.ContentLength = int64(len(rewritten))
	response.Header.Del("Transfer-Encoding")
	response.TransferEncoding = nil
	response.Header.Set("Content-Length", strconv.Itoa(len(rewritten)))
	return nil
}

func (proxy *DockerProxy) Close(ctx context.Context) error {
	if proxy.server == nil {
		return nil
	}
	return proxy.server.Shutdown(ctx)
}

func waitForUnixSocket(ctx context.Context, path string) error {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		connection, err := net.DialTimeout("unix", path, 500*time.Millisecond)
		if err == nil {
			_ = connection.Close()
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func mirrorSocketPermissions(upstream, frontend string) error {
	info, err := os.Stat(upstream)
	if err != nil {
		return fmt.Errorf("inspect upstream Docker socket: %w", err)
	}
	if err := os.Chmod(frontend, info.Mode().Perm()); err != nil {
		return fmt.Errorf("set Docker socket permissions: %w", err)
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		if err := os.Chown(frontend, int(stat.Uid), int(stat.Gid)); err != nil {
			return fmt.Errorf("set Docker socket ownership: %w", err)
		}
	}
	return nil
}
