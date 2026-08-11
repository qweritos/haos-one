package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/qweritos/haos-one/internal/netagent"
)

var version = "dev"

type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }
func (s *stringList) Set(value string) error {
	*s = append(*s, value)
	return nil
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("haos-one-net: ")
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "haos-one-net:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		usage()
		return errors.New("a command is required")
	}
	switch args[0] {
	case "init":
		return runInit(args[1:])
	case "host":
		if len(args) < 2 || args[1] != "run" {
			return errors.New("usage: haos-one-net host run [--config PATH]")
		}
		return runAgent("host", args[2:])
	case "guest":
		if len(args) < 2 || args[1] != "run" {
			return errors.New("usage: haos-one-net guest run [--config PATH]")
		}
		return runAgent("guest", args[2:])
	case "doctor":
		return runDoctor(args[1:])
	case "cleanup":
		return runCleanup(args[1:])
	case "version", "--version", "-version":
		fmt.Println(version)
		return nil
	case "help", "--help", "-h":
		usage()
		return nil
	default:
		usage()
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runInit(args []string) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	runtimeName := fs.String("runtime", "auto", "auto, docker-desktop, or colima")
	output := fs.String("output-dir", "", "configuration directory")
	endpoint := fs.String("host-endpoint", netagent.DefaultHostEndpoint, "host name or IPv4 address visible inside the container")
	listenPort := fs.Int("listen-port", netagent.DefaultListenPort, "host WireGuard UDP port")
	tunnelCIDR := fs.String("tunnel-cidr", "auto", "IPv4 /30 or auto")
	force := fs.Bool("force", false, "replace existing configuration and rotate keys")
	var interfaces, cidrs stringList
	fs.Var(&interfaces, "lan-interface", "LAN interface (repeatable)")
	fs.Var(&cidrs, "lan-cidr", "LAN IPv4 CIDR (repeatable)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	result, err := netagent.Init(netagent.InitOptions{Runtime: *runtimeName, OutputDir: *output, HostEndpoint: *endpoint, ListenPort: *listenPort, TunnelCIDR: *tunnelCIDR, Interfaces: interfaces, LANCIDRs: cidrs, Force: *force})
	if err != nil {
		return err
	}
	fmt.Printf("Generated host config: %s\nGenerated guest config: %s\n\n%s", result.HostPath, result.GuestPath, netagent.DockerSnippets(result))
	return nil
}

func runAgent(role string, args []string) error {
	fs := flag.NewFlagSet(role+" run", flag.ContinueOnError)
	defaultPath, _ := netagent.DefaultConfigDir()
	if role == "guest" {
		defaultPath = "/etc/haos-one/desktop-network.yaml"
	} else {
		defaultPath = filepath.Join(defaultPath, "host.yaml")
	}
	configPath := fs.String("config", defaultPath, "configuration file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := netagent.LoadConfig(*configPath)
	if err != nil {
		return err
	}
	if cfg.Role != role {
		return fmt.Errorf("configuration role is %s, expected %s", cfg.Role, role)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if role == "host" {
		return netagent.RunHost(ctx, cfg)
	}
	return netagent.RunGuest(ctx, cfg)
}

func runDoctor(args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	defaultDir, _ := netagent.DefaultConfigDir()
	configPath := fs.String("config", filepath.Join(defaultDir, "host.yaml"), "host or guest configuration")
	container := fs.String("container", "", "HAOS One container name")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := netagent.LoadConfig(*configPath)
	if err != nil {
		return err
	}
	failed := false
	for _, check := range netagent.Doctor(context.Background(), cfg, *container) {
		mark := "OK"
		if !check.OK {
			mark = "FAIL"
			failed = true
		}
		fmt.Printf("%-4s %-30s %s\n", mark, check.Name, check.Detail)
	}
	if failed {
		return errors.New("one or more checks failed")
	}
	return nil
}

func runCleanup(args []string) error {
	fs := flag.NewFlagSet("cleanup", flag.ContinueOnError)
	defaultDir, _ := netagent.DefaultConfigDir()
	configPath := fs.String("config", filepath.Join(defaultDir, "host.yaml"), "host configuration")
	purge := fs.Bool("purge", false, "also remove generated host and guest configuration")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := netagent.LoadConfig(*configPath)
	if err != nil {
		return err
	}
	guestPath := filepath.Join(filepath.Dir(*configPath), "guest.yaml")
	return netagent.Cleanup(context.Background(), cfg, *purge, *configPath, guestPath)
}

func usage() {
	fmt.Print(`haos-one-net manages host-assisted discovery for HAOS One.

Commands:
  init       generate host and guest configuration
  host run   run the macOS/Windows host gateway and discovery relay
  guest run  run the in-container Linux tunnel and relay
  doctor     inspect configuration and live connectivity
  cleanup    remove managed host networking state
  version    print the build version
`)
}
