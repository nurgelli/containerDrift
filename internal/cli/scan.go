package cli

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/nurgelli/containerdrift/internal/compose"
	"github.com/nurgelli/containerdrift/internal/docker"
	"github.com/nurgelli/containerdrift/internal/drift"
	"github.com/nurgelli/containerdrift/internal/model"
	"github.com/nurgelli/containerdrift/internal/report"
)

// ErrDriftDetected is returned when the scan found drift. It is not a failure
// of the tool itself, so main maps it to exit code 1 without printing it.
var ErrDriftDetected = errors.New("drift detected")

var errInvalidFormat = errors.New("invalid output format")

// desiredReader and actualReader are the two inputs of a scan. They are
// defined here, where they are used, so tests can replace them with fakes.
type desiredReader interface {
	Read(ctx context.Context, opts compose.Options) (model.Project, error)
}

type actualReader interface {
	ListProject(ctx context.Context, project string) ([]model.ContainerState, []string, error)
}

// scanDeps holds the collaborators of one scan.
type scanDeps struct {
	desired desiredReader
	actual  actualReader
	close   func() error
}

// depsFactory creates the collaborators lazily, so that commands such as
// --help never touch Docker.
type depsFactory func() (scanDeps, error)

func defaultDeps() (scanDeps, error) {
	inspector, err := docker.New()
	if err != nil {
		return scanDeps{}, err
	}
	return scanDeps{desired: compose.NewReader(), actual: inspector, close: inspector.Close}, nil
}

type scanOptions struct {
	files       []string
	projectName string
	envFiles    []string
	profiles    []string
	format      string
	strict      bool
}

func (o scanOptions) validate() error {
	switch o.format {
	case "table", "json":
		return nil
	default:
		return fmt.Errorf("%w: %q (want \"table\" or \"json\")", errInvalidFormat, o.format)
	}
}

func (o scanOptions) composeOptions() compose.Options {
	return compose.Options{
		Files:       o.files,
		ProjectName: o.projectName,
		EnvFiles:    o.envFiles,
		Profiles:    o.profiles,
	}
}

func newScanCmd(version string, deps depsFactory) *cobra.Command {
	var opts scanOptions

	cmd := &cobra.Command{
		Use:   "scan",
		Short: "Compare the Compose configuration with running containers",
		Long: "Scan reads the Compose configuration (via \"docker compose config\") and the containers of\n" +
			"the same project (via the Docker Engine API) and reports every difference.\n\n" +
			"Pass the same -f, -p, --env-file and --profile flags you use with \"docker compose up\",\n" +
			"otherwise the desired state may differ from what you deployed.\n\n" +
			"Exit codes: 0 no drift, 1 drift detected, 2 the scan could not be completed.",
		Example: "  containerdrift scan\n" +
			"  containerdrift scan -f compose.yaml -f compose.prod.yaml -p myapp\n" +
			"  containerdrift scan --format json > drift.json",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.validate(); err != nil {
				return err
			}
			d, err := deps()
			if err != nil {
				return err
			}
			if d.close != nil {
				defer d.close()
			}
			return runScan(cmd.Context(), cmd.OutOrStdout(), version, opts, d)
		},
	}

	f := cmd.Flags()
	f.StringArrayVarP(&opts.files, "file", "f", nil, "Compose file (repeatable)")
	f.StringVarP(&opts.projectName, "project-name", "p", "", "Compose project name")
	f.StringArrayVar(&opts.envFiles, "env-file", nil, "environment file (repeatable)")
	f.StringArrayVar(&opts.profiles, "profile", nil, "Compose profile to enable (repeatable)")
	f.StringVar(&opts.format, "format", "table", "output format: table or json")
	f.BoolVar(&opts.strict, "strict", false, "exit with code 1 on warnings as well as on drift")

	return cmd
}

// runScan reads both states, compares them, writes the report and returns
// ErrDriftDetected when the exit code should be 1.
func runScan(ctx context.Context, out io.Writer, version string, opts scanOptions, d scanDeps) error {
	desired, err := d.desired.Read(ctx, opts.composeOptions())
	if err != nil {
		return fmt.Errorf("reading Compose configuration: %w", err)
	}

	containers, warnings, err := d.actual.ListProject(ctx, desired.Name)
	if err != nil {
		return fmt.Errorf("reading Docker state: %w", err)
	}

	result := drift.Compare(desired, containers, warnings)

	switch opts.format {
	case "json":
		err = report.WriteJSON(out, version, result)
	default:
		err = report.WriteTable(out, version, result)
	}
	if err != nil {
		return fmt.Errorf("writing report: %w", err)
	}

	s := result.Summary()
	if s.Drift > 0 || (opts.strict && (s.Warnings > 0 || len(result.Warnings) > 0)) {
		return ErrDriftDetected
	}
	return nil
}
