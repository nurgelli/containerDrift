package cli

import (
	"context"
	"github.com/spf13/cobra"
)

// NewRootCmd builds the root command. It is a function, not a package-level
// variable, so each test can create its own fresh command tree.
func NewRootCmd(version string) *cobra.Command {
	return &cobra.Command{
		Use:     "containerdrift",
		Short:   "Detect drift between Docker Compose config and running containers",
		Version: version,
		// main prints errors itself, so Cobra must not print them (or the
		// usage text) a second time.
		SilenceErrors: true,
		SilenceUsage:  true,
	}
}

// Execute runs the CLI and returns the first error, if any.
func Execute(ctx context.Context, version string) error {
	return NewRootCmd(version).ExecuteContext(ctx)
}