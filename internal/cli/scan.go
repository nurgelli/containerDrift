package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
)

var (
	errNotImplemented = errors.New("scan is not implemented yet")
	errInvalidFormat  = errors.New("invalid output format")
)

type scanOptions struct {
	files       []string
	projectName string
	envFiles    []string
	profiles    []string
	format      string
}

func (o scanOptions) validate() error {
	switch o.format {
	case "table", "json":
		return nil
	default:
		return fmt.Errorf("%w: %q (want \"table\" or \"json\")", errInvalidFormat, o.format)
	}
}

func newScanCmd() *cobra.Command {
	var opts scanOptions

	cmd := &cobra.Command{
		Use:   "scan",
		Short: "Compare the Compose configuration with running containers",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.validate(); err != nil {
				return err
			}
			return errNotImplemented
		},
	}

	f := cmd.Flags()
	f.StringArrayVarP(&opts.files, "file", "f", nil, "Compose file (repeatable)")
	f.StringVarP(&opts.projectName, "project-name", "p", "", "Compose project name")
	f.StringArrayVar(&opts.envFiles, "env-file", nil, "environment file (repeatable)")
	f.StringArrayVar(&opts.profiles, "profile", nil, "Compose profile to enable (repeatable)")
	f.StringVar(&opts.format, "format", "table", "output format: table or json")

	return cmd
}
