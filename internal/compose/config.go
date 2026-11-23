package compose

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// configTimeout bounds how long "docker compose config" may run.
const configTimeout = 30 * time.Second

// Options selects which Compose configuration to read. It mirrors the flags
// of "docker compose" so that the result matches what "up" would use.
type Options struct {
	Files       []string
	ProjectName string
	EnvFiles    []string
	Profiles    []string
}

// The raw* types describe only the fields ContainerDrift needs. encoding/json
// ignores every other field, including "environment", so resolved secrets
// never enter our data structures.
type rawProject struct {
	Name     string                `json:"name"`
	Services map[string]rawService `json:"services"`
	Volumes  map[string]rawVolume  `json:"volumes"`
}

type rawService struct {
	Image      string     `json:"image"`
	Build      *rawBuild  `json:"build"` // non-nil when the service has a build section
	Ports      []rawPort  `json:"ports"`
	Volumes    []rawMount `json:"volumes"`
	Restart    string     `json:"restart"`
	Privileged bool       `json:"privileged"`
}

type rawBuild struct{}

type rawPort struct {
	Target    int    `json:"target"`
	Published string `json:"published"` // a string in the Compose output we observed
	Protocol  string `json:"protocol"`
	HostIP    string `json:"host_ip"`
}

type rawMount struct {
	Type     string `json:"type"`
	Source   string `json:"source"`
	Target   string `json:"target"`
	ReadOnly bool   `json:"read_only"`
}

type rawVolume struct {
	Name string `json:"name"`
}

// configArgs builds the docker arguments. Compose's global flags must come
// before the "config" subcommand.
func configArgs(opts Options) []string {
	args := []string{"compose"}
	for _, f := range opts.Files {
		args = append(args, "-f", f)
	}
	if opts.ProjectName != "" {
		args = append(args, "-p", opts.ProjectName)
	}
	for _, e := range opts.EnvFiles {
		args = append(args, "--env-file", e)
	}
	for _, p := range opts.Profiles {
		args = append(args, "--profile", p)
	}
	return append(args, "config", "--format", "json")
}

// loadRaw runs "docker compose config" and decodes its JSON output.
func (r *Reader) loadRaw(ctx context.Context, opts Options) (rawProject, error) {
	ctx, cancel := context.WithTimeout(ctx, configTimeout)
	defer cancel()

	out, err := r.run(ctx, configArgs(opts)...)
	if err != nil {
		return rawProject{}, fmt.Errorf("reading compose configuration: %w", err)
	}
	return decodeConfig(out)
}

func decodeConfig(data []byte) (rawProject, error) {
	var p rawProject
	if err := json.Unmarshal(data, &p); err != nil {
		// The error is wrapped without the input: the output contains
		// resolved environment values.
		return rawProject{}, fmt.Errorf("decoding compose configuration: %w", err)
	}
	if p.Name == "" {
		return rawProject{}, errors.New("compose configuration has no project name")
	}
	return p, nil
}
