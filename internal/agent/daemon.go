package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/qweritos/haos-one/internal/netagent"
)

const (
	DefaultGuestConfig    = "/etc/haos-one/desktop-network.yaml"
	DefaultDockerSocket   = "/run/docker.sock"
	DefaultDockerUpstream = "/run/docker-real.sock"
	versionStatePath      = "/run/haos-one-agent/version.json"
)

type Options struct {
	Version               string
	GuestConfig           string
	DockerSocket          string
	DockerUpstream        string
	ProjectionPath        string
	DisableNetworkManager bool
}

type VersionState struct {
	Version  string `json:"version"`
	Protocol int    `json:"protocol"`
}

func Run(ctx context.Context, options Options) error {
	options = options.withDefaults()
	if err := writeVersionState(options.Version); err != nil {
		return err
	}
	defer os.Remove(versionStatePath)

	udevEnabled, err := UdevShimEnabledFromEnvironment()
	if err != nil {
		return err
	}
	log.Printf("haos-one-agent: version=%s protocol=%d udev_shim=%t", options.Version, netagent.ProtocolVersion, udevEnabled)

	dockerReady := make(chan struct{})
	nmReady := make(chan struct{})
	proxy := &DockerProxy{Frontend: options.DockerSocket, Upstream: options.DockerUpstream, InjectUdev: udevEnabled}
	var workers sync.WaitGroup
	workers.Add(1)
	go func() {
		defer workers.Done()
		supervise(ctx, "Docker proxy", func(runCtx context.Context, ready func()) error {
			return proxy.Run(runCtx, ready)
		}, dockerReady)
	}()

	if options.DisableNetworkManager {
		close(nmReady)
	} else {
		workers.Add(1)
		go func() {
			defer workers.Done()
			service := &NetworkManagerService{ProjectionPath: options.ProjectionPath}
			supervise(ctx, "NetworkManager", service.Run, nmReady)
		}()
	}

	workers.Add(1)
	go func() {
		defer workers.Done()
		runNetworkReconciler(ctx, options.GuestConfig)
	}()

	select {
	case <-ctx.Done():
		workers.Wait()
		return nil
	case <-dockerReady:
	}
	if err := migrateUntilReady(ctx, options.DockerUpstream, udevEnabled); err != nil {
		workers.Wait()
		return err
	}
	select {
	case <-ctx.Done():
		workers.Wait()
		return nil
	case <-nmReady:
	}
	if err := notifySystemd("READY=1\nSTATUS=HAOS One guest agent is ready"); err != nil {
		log.Printf("haos-one-agent: systemd readiness notification: %v", err)
	}
	go migrationReconciler(ctx, options.DockerUpstream, udevEnabled)

	<-ctx.Done()
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	_ = proxy.Close(shutdown)
	cancel()
	workers.Wait()
	return nil
}

func (options Options) withDefaults() Options {
	if options.Version == "" {
		options.Version = "dev"
	}
	if options.GuestConfig == "" {
		options.GuestConfig = DefaultGuestConfig
	}
	if options.DockerSocket == "" {
		options.DockerSocket = DefaultDockerSocket
	}
	if options.DockerUpstream == "" {
		options.DockerUpstream = DefaultDockerUpstream
	}
	if options.ProjectionPath == "" {
		options.ProjectionPath = projectionPath
	}
	return options
}

func supervise(ctx context.Context, name string, run func(context.Context, func()) error, firstReady chan struct{}) {
	backoff := time.Second
	var readyOnce sync.Once
	for ctx.Err() == nil {
		started := time.Now()
		err := run(ctx, func() {
			readyOnce.Do(func() { close(firstReady) })
			backoff = time.Second
		})
		if ctx.Err() != nil {
			return
		}
		if err == nil {
			err = errors.New("component exited unexpectedly")
		}
		log.Printf("haos-one-agent: %s failed: %v; retrying in %s", name, err, backoff)
		if time.Since(started) > 30*time.Second {
			backoff = time.Second
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
		if backoff > 30*time.Second {
			backoff = 30 * time.Second
		}
	}
}

func runNetworkReconciler(ctx context.Context, configPath string) {
	for ctx.Err() == nil {
		info, err := os.Stat(configPath)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				log.Printf("haos-one-agent: inspect guest network config: %v", err)
			}
			if !waitContext(ctx, 2*time.Second) {
				return
			}
			continue
		}
		cfg, err := netagent.LoadConfig(configPath)
		if err != nil || cfg.Role != "guest" {
			if err == nil {
				err = fmt.Errorf("configuration role is %s, expected guest", cfg.Role)
			}
			log.Printf("haos-one-agent: guest networking disabled: %v", err)
			if !waitContext(ctx, 3*time.Second) {
				return
			}
			continue
		}
		runCtx, cancel := context.WithCancel(ctx)
		done := make(chan error, 1)
		go func() { done <- netagent.RunGuest(runCtx, cfg) }()
		ticker := time.NewTicker(2 * time.Second)
		restart := false
		for !restart {
			select {
			case <-ctx.Done():
				cancel()
				ticker.Stop()
				<-done
				return
			case runErr := <-done:
				if runErr != nil && ctx.Err() == nil {
					log.Printf("haos-one-agent: guest networking failed: %v", runErr)
				}
				restart = true
			case <-ticker.C:
				current, statErr := os.Stat(configPath)
				if statErr != nil || current.ModTime() != info.ModTime() || current.Size() != info.Size() {
					cancel()
					<-done
					restart = true
				}
			}
		}
		ticker.Stop()
		cancel()
		if !waitContext(ctx, time.Second) {
			return
		}
	}
}

func migrateUntilReady(ctx context.Context, socket string, enabled bool) error {
	for {
		migrated, err := MigrateSupervisorUdev(ctx, socket, enabled)
		if err == nil {
			if migrated {
				log.Printf("haos-one-agent: recreated hassio_supervisor to enable the udev shim")
			}
			return nil
		}
		log.Printf("haos-one-agent: initial Supervisor migration: %v", err)
		if !waitContext(ctx, time.Second) {
			return ctx.Err()
		}
	}
}

func migrationReconciler(ctx context.Context, socket string, enabled bool) {
	if !enabled {
		return
	}
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			migrated, err := MigrateSupervisorUdev(ctx, socket, enabled)
			if err != nil {
				log.Printf("haos-one-agent: reconcile Supervisor udev shim: %v", err)
			}
			if migrated {
				log.Printf("haos-one-agent: recreated hassio_supervisor to repair the udev shim")
			}
		}
	}
}

func waitContext(ctx context.Context, duration time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(duration):
		return true
	}
}

func writeVersionState(version string) error {
	payload, err := json.Marshal(VersionState{Version: version, Protocol: netagent.ProtocolVersion})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(versionStatePath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(versionStatePath, append(payload, '\n'), 0o644)
}
