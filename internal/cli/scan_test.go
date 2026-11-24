package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/nurgelli/containerdrift/internal/compose"
	"github.com/nurgelli/containerdrift/internal/docker"
	"github.com/nurgelli/containerdrift/internal/model"
)

type fakeDesired struct {
	project model.Project
	err     error
	gotOpts compose.Options
}

func (f *fakeDesired) Read(_ context.Context, opts compose.Options) (model.Project, error) {
	f.gotOpts = opts
	return f.project, f.err
}

type fakeActual struct {
	containers []model.ContainerState
	warnings   []string
	err        error
	gotProject string
}

func (f *fakeActual) ListProject(_ context.Context, project string) ([]model.ContainerState, []string, error) {
	f.gotProject = project
	return f.containers, f.warnings, f.err
}

func webSpec() model.ServiceSpec {
	return model.ServiceSpec{Name: "web", Image: "nginx:alpine", Restart: model.RestartPolicy{Name: "no"}}
}

func webContainer() model.ContainerState {
	return model.ContainerState{
		Name: "demo-web-1", Service: "web", State: "running",
		Image: "nginx:alpine", Restart: model.RestartPolicy{Name: "no"},
	}
}

// run executes the root command with the given collaborators and returns the
// standard output and the error.
func run(t *testing.T, d *fakeDesired, a *fakeActual, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	created, closed := false, false
	factory := func() (scanDeps, error) {
		created = true
		return scanDeps{desired: d, actual: a, close: func() error { closed = true; return nil }}, nil
	}

	cmd := newRootCmd("0.1.0", factory)
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs(args)

	err := cmd.Execute()
	// Whenever the collaborators were created, they must also be closed.
	if created && !closed {
		t.Error("dependencies were not closed")
	}
	return out.String(), err
}

func TestScanNoDrift(t *testing.T) {
	d := &fakeDesired{project: model.Project{Name: "demo", Services: []model.ServiceSpec{webSpec()}}}
	a := &fakeActual{containers: []model.ContainerState{webContainer()}}

	out, err := run(t, d, a, "scan")
	if err != nil {
		t.Fatalf("scan error = %v", err)
	}
	for _, want := range []string{"ContainerDrift v0.1.0", "Project: demo", "[PASS] web", "Scan completed."} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if a.gotProject != "demo" {
		t.Errorf("containers were listed for project %q, want the Compose project name", a.gotProject)
	}
}

func TestScanDrift(t *testing.T) {
	c := webContainer()
	c.Image = "nginx:1.25"
	d := &fakeDesired{project: model.Project{Name: "demo", Services: []model.ServiceSpec{webSpec()}}}
	a := &fakeActual{containers: []model.ContainerState{c}}

	out, err := run(t, d, a, "scan")
	if !errors.Is(err, ErrDriftDetected) {
		t.Fatalf("error = %v, want ErrDriftDetected", err)
	}
	if !strings.Contains(out, "[DRIFT] web") || !strings.Contains(out, "nginx:1.25") {
		t.Errorf("report does not show the drift:\n%s", out)
	}
}

func TestScanJSON(t *testing.T) {
	d := &fakeDesired{project: model.Project{Name: "demo", Services: []model.ServiceSpec{webSpec()}}}
	a := &fakeActual{containers: []model.ContainerState{webContainer()}}

	out, err := run(t, d, a, "scan", "--format", "json")
	if err != nil {
		t.Fatalf("scan error = %v", err)
	}
	var got struct {
		SchemaVersion string `json:"schemaVersion"`
		Project       string `json:"project"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out)
	}
	if got.SchemaVersion == "" || got.Project != "demo" {
		t.Errorf("decoded = %+v", got)
	}
}

func TestScanPassesFlagsToCompose(t *testing.T) {
	d := &fakeDesired{project: model.Project{Name: "demo"}}
	a := &fakeActual{}

	_, err := run(t, d, a, "scan", "-f", "a.yaml", "-f", "b.yaml", "-p", "demo",
		"--env-file", ".env", "--profile", "debug")
	if err != nil {
		t.Fatalf("scan error = %v", err)
	}
	want := compose.Options{
		Files: []string{"a.yaml", "b.yaml"}, ProjectName: "demo",
		EnvFiles: []string{".env"}, Profiles: []string{"debug"},
	}
	if !slices.Equal(d.gotOpts.Files, want.Files) || d.gotOpts.ProjectName != want.ProjectName ||
		!slices.Equal(d.gotOpts.EnvFiles, want.EnvFiles) || !slices.Equal(d.gotOpts.Profiles, want.Profiles) {
		t.Errorf("options = %+v, want %+v", d.gotOpts, want)
	}
}

func TestScanStrictTreatsWarningsAsFailure(t *testing.T) {
	d := &fakeDesired{project: model.Project{Name: "demo", Services: []model.ServiceSpec{webSpec()}}}
	a := &fakeActual{containers: []model.ContainerState{webContainer()}, warnings: []string{"container x disappeared"}}

	if _, err := run(t, d, a, "scan"); err != nil {
		t.Errorf("warnings alone must not fail without --strict, got %v", err)
	}
	if _, err := run(t, d, a, "scan", "--strict"); !errors.Is(err, ErrDriftDetected) {
		t.Errorf("--strict error = %v, want ErrDriftDetected", err)
	}
}

func TestScanErrors(t *testing.T) {
	tests := []struct {
		name    string
		d       *fakeDesired
		a       *fakeActual
		args    []string
		wantErr error
	}{
		{"compose unavailable", &fakeDesired{err: compose.ErrUnavailable}, &fakeActual{}, []string{"scan"}, compose.ErrUnavailable},
		{"docker unavailable", &fakeDesired{project: model.Project{Name: "demo"}}, &fakeActual{err: docker.ErrUnavailable}, []string{"scan"}, docker.ErrUnavailable},
		{"invalid format", &fakeDesired{}, &fakeActual{}, []string{"scan", "--format", "xml"}, errInvalidFormat},
		{"positional argument", &fakeDesired{}, &fakeActual{}, []string{"scan", "extra"}, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := run(t, tt.d, tt.a, tt.args...)
			if err == nil {
				t.Fatal("scan succeeded, want error")
			}
			if errors.Is(err, ErrDriftDetected) {
				t.Errorf("an operational error must not look like drift: %v", err)
			}
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Errorf("error = %v, want one wrapping %v", err, tt.wantErr)
			}
		})
	}
}
