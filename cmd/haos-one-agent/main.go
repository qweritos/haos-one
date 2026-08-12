package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/qweritos/haos-one/internal/agent"
	"github.com/qweritos/haos-one/internal/netagent"
)

var version = "dev"

func main() {
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	if len(os.Args) > 1 {
		if len(os.Args) == 2 && os.Args[1] == "--version" {
			fmt.Printf("%s (protocol %d)\n", version, netagent.ProtocolVersion)
			return
		}
		fmt.Fprintln(os.Stderr, "haos-one-agent: this executable is a daemon and accepts no commands")
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := agent.Run(ctx, agent.Options{Version: version, DisableNetworkManager: !environmentEnabled("USE_DUMMY_NETWORKMANAGER", true)}); err != nil {
		fmt.Fprintln(os.Stderr, "haos-one-agent:", err)
		os.Exit(1)
	}
}

func environmentEnabled(name string, defaultValue bool) bool {
	value, exists := os.LookupEnv(name)
	if !exists {
		return defaultValue
	}
	switch value {
	case "1", "true", "TRUE", "yes", "YES", "on", "ON":
		return true
	default:
		return false
	}
}
