// Command containerdrift detects drift between a Docker Compose configuration
// and the containers that are actually running.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/nurgelli/containerdrift/internal/cli"
)

// version is set at build time: go build -ldflags "-X main.version=0.1.0".
var version = "dev"

// Exit codes.
const (
	exitOK    = 0
	exitDrift = 1
	exitError = 2
)

func main() {
	os.Exit(run())
}

// run exists so that deferred calls execute before the process exits:
// os.Exit terminates immediately and skips defers.
func run() int {
	// Cancel the context on Ctrl+C or SIGTERM so Docker and Compose calls stop.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	err := cli.Execute(ctx, version)
	switch {
	case err == nil:
		return exitOK
	case errors.Is(err, cli.ErrDriftDetected):
		// The report has already been printed.
		return exitDrift
	default:
		fmt.Fprintln(os.Stderr, "error:", err)
		return exitError
	}
}
