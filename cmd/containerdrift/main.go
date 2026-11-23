package main

import (
	"context"
	"fmt"
	"github.com/nurgelli/containerdrift/internal/cli"
	"os"
	"os/signal"
)

// version is set at build time: go build -ldflags "-X main.version=0.1.0".
var version = "dev"

func main() {
	os.Exit(run())
}

// run exists so that deferred calls execute before the process exits:
// os.Exit terminates immediately and skips defers.
func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if err := cli.Execute(ctx, version); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 2
	}
	return 0
}
