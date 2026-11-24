// Package cli wires the command line to the Compose reader, the Docker
// inspector, the drift engine and the reporters.
package cli

import (
	"context"

	"github.com/spf13/cobra"
)

// NewRootCmd builds the root command. It is a function, not a package-level
// variable, so each test can create its own fresh command tree.
func NewRootCmd(version string) *cobra.Command {
	return newRootCmd(version, defaultDeps)
}

func newRootCmd(version string, deps depsFactory) *cobra.Command {
	root := &cobra.Command{
		Use:   "containerdrift",
		Short: "Detect drift between Docker Compose config and running containers",
		Long: "ContainerDrift compares the Docker Compose configuration with the containers that are\n" +
			"actually running and reports the differences. It is read-only: it never starts, stops,\n" +
			"recreates or modifies anything.",
		Version: version,
		// main prints errors itself, so Cobra must not print them (or the
		// usage text) a second time.
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	root.AddCommand(newScanCmd(version, deps))
	return root
}

// Execute runs the CLI and returns the first error, if any.
func Execute(ctx context.Context, version string) error {
	return NewRootCmd(version).ExecuteContext(ctx)
}
