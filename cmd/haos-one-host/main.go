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

func (s *stringList) String() string         { return strings.Join(*s, ",") }
func (s *stringList) Set(value string) error { *s = append(*s, value); return nil }

func main() {
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("haos-one-host: ")
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "haos-one-host:", err)
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
	case "run":
		return runHost(args[1:])
	case "doctor":
		return runDoctor(args[1:])
	case "cleanup":
		return runCleanup(args[1:])
	case "version", "--version", "-version":
		fmt.Printf("%s (protocol %d)\n", version, netagent.ProtocolVersion)
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
	dnsName := fs.String("dns-name", netagent.DefaultDNSName, "host-advertised .local DNS name")
	var interfaces, cidrs stringList
	fs.Var(&interfaces, "lan-interface", "LAN interface (repeatable)")
	fs.Var(&cidrs, "lan-cidr", "LAN IPv4 CIDR (repeatable)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	result, err := netagent.Init(netagent.InitOptions{Runtime: *runtimeName, OutputDir: *output, HostEndpoint: *endpoint, ListenPort: *listenPort, TunnelCIDR: *tunnelCIDR, Interfaces: interfaces, LANCIDRs: cidrs, Force: *force, DNSName: *dnsName})
	if err != nil {
		return err
	}
	fmt.Printf("Generated host config: %s\nGenerated guest config: %s\n\n%s", result.HostPath, result.GuestPath, netagent.DockerSnippets(result))
	return nil
}

func runHost(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	configPath := fs.String("config", "", "configuration file (or "+netagent.ConfigPathEnv+")")
	if err := fs.Parse(args); err != nil {
		return err
	}
	resolvedPath, err := netagent.ResolveConfigPath(*configPath, "host")
	if err != nil {
		return err
	}
	cfg, err := netagent.LoadConfig(resolvedPath)
	if err != nil {
		return err
	}
	if cfg.Role != "host" {
		return fmt.Errorf("configuration role is %s, expected host", cfg.Role)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return netagent.RunHost(ctx, cfg)
}

func runDoctor(args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	configPath := fs.String("config", "", "host configuration (or "+netagent.ConfigPathEnv+")")
	container := fs.String("container", "", "HAOS One container name")
	if err := fs.Parse(args); err != nil {
		return err
	}
	resolvedPath, err := netagent.ResolveConfigPath(*configPath, "host")
	if err != nil {
		return err
	}
	cfg, err := netagent.LoadConfig(resolvedPath)
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
	configPath := fs.String("config", "", "host configuration (or "+netagent.ConfigPathEnv+")")
	purge := fs.Bool("purge", false, "also remove generated host and guest configuration")
	if err := fs.Parse(args); err != nil {
		return err
	}
	resolvedPath, err := netagent.ResolveConfigPath(*configPath, "host")
	if err != nil {
		return err
	}
	cfg, err := netagent.LoadConfig(resolvedPath)
	if err != nil {
		return err
	}
	guestPath := filepath.Join(filepath.Dir(resolvedPath), "guest.yaml")
	return netagent.Cleanup(context.Background(), cfg, *purge, resolvedPath, guestPath)
}

func usage() {
	fmt.Print(`haos-one-host manages the HAOS One host networking companion.

Commands:
  init       generate host and guest configuration
  run        run the macOS/Windows host gateway and discovery relay
  doctor     inspect configuration and live connectivity
  cleanup    remove managed host networking state
  version    print build and relay protocol versions
`)
}
