package compose

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
)

func TestConfigArgs(t *testing.T) {
	tests := []struct {
		name string
		opts Options
		want []string
	}{
		{
			name: "defaults",
			opts: Options{},
			want: []string{"compose", "config", "--format", "json"},
		},
		{
			name: "all options",
			opts: Options{
				Files:       []string{"a.yaml", "b.yaml"},
				ProjectName: "demo",
				EnvFiles:    []string{".env.prod"},
				Profiles:    []string{"debug"},
			},
			want: []string{
				"compose", "-f", "a.yaml", "-f", "b.yaml", "-p", "demo",
				"--env-file", ".env.prod", "--profile", "debug",
				"config", "--format", "json",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := configArgs(tt.opts); !slices.Equal(got, tt.want) {
				t.Errorf("configArgs() = %v, want %v", got, tt.want)
			}
		})
	}
}

func readFixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/config_full.json")
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	return data
}

func TestDecodeConfigFixture(t *testing.T) {
	p, err := decodeConfig(readFixture(t))
	if err != nil {
		t.Fatalf("decodeConfig() error = %v", err)
	}

	if p.Name != "demo" || len(p.Services) != 2 {
		t.Fatalf("got name %q with %d services", p.Name, len(p.Services))
	}

	db := p.Services["db"]
	if db.Image != "postgres:15-alpine" {
		t.Errorf("db image = %q", db.Image)
	}
	if len(db.Ports) != 1 || db.Ports[0].Published != "5432" || db.Ports[0].Target != 5432 {
		t.Errorf("db ports = %+v", db.Ports)
	}
	if db.Restart != "unless-stopped" || !db.Privileged {
		t.Errorf("db restart = %q, privileged = %v", db.Restart, db.Privileged)
	}
	if db.Build != nil {
		t.Error("db has no build section but Build is non-nil")
	}
	if p.Volumes["db_data"].Name != "demo_db_data" {
		t.Errorf("resolved volume name = %q", p.Volumes["db_data"].Name)
	}

	if p.Services["api"].Build == nil {
		t.Error("api has a build section but Build is nil")
	}
}

func TestDecodeConfigDoesNotRetainEnvironment(t *testing.T) {
	p, err := decodeConfig(readFixture(t))
	if err != nil {
		t.Fatalf("decodeConfig() error = %v", err)
	}
	if strings.Contains(fmt.Sprintf("%+v", p), "SUPERSECRET") {
		t.Error("decoded configuration contains environment values")
	}
}

func TestDecodeConfigErrors(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{"empty output", ""},
		{"not json", "hello"},
		{"missing project name", `{"services":{}}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := decodeConfig([]byte(tt.in)); err == nil {
				t.Error("decodeConfig() succeeded, want error")
			}
		})
	}
}

func TestLoadRaw(t *testing.T) {
	var gotArgs []string
	var hasDeadline bool

	r := &Reader{run: func(ctx context.Context, args ...string) ([]byte, error) {
		gotArgs = args
		_, hasDeadline = ctx.Deadline()
		return []byte(`{"name":"demo","services":{}}`), nil
	}}

	p, err := r.loadRaw(context.Background(), Options{ProjectName: "demo"})
	if err != nil {
		t.Fatalf("loadRaw() error = %v", err)
	}
	if p.Name != "demo" {
		t.Errorf("project name = %q", p.Name)
	}

	want := []string{"compose", "-p", "demo", "config", "--format", "json"}
	if !slices.Equal(gotArgs, want) {
		t.Errorf("runner args = %v, want %v", gotArgs, want)
	}
	if !hasDeadline {
		t.Error("runner context has no deadline")
	}
}

func TestLoadRawCommandFailure(t *testing.T) {
	cmdErr := &CommandError{
		Args:     []string{"compose", "config"},
		ExitCode: 1,
		Stderr:   "no configuration file provided",
	}
	r := &Reader{run: func(context.Context, ...string) ([]byte, error) {
		return nil, cmdErr
	}}

	_, err := r.loadRaw(context.Background(), Options{})

	var got *CommandError
	if !errors.As(err, &got) || got.ExitCode != 1 {
		t.Errorf("error %v does not wrap the *CommandError", err)
	}
}

// TestExecRunnerDockerMissing runs the real execRunner, without Docker.
func TestExecRunnerDockerMissing(t *testing.T) {
	t.Setenv("PATH", "")

	_, err := execRunner(context.Background(), "compose", "version")
	if !errors.Is(err, ErrUnavailable) {
		t.Errorf("error = %v, want one wrapping ErrUnavailable", err)
	}
}

func TestDecodeConfigRealOutput(t *testing.T) {
	data, err := os.ReadFile("testdata/config_web.json")
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}

	p, err := decodeConfig(data)
	if err != nil {
		t.Fatalf("decodeConfig() error = %v", err)
	}

	web, ok := p.Services["web"]
	if !ok {
		t.Fatal("service web not found")
	}

	wantPorts := []rawPort{
		{Target: 80, Published: "8080", Protocol: "tcp"},
		{Target: 90, Published: "9090", Protocol: "udp", HostIP: "127.0.0.1"},
	}
	if !slices.Equal(web.Ports, wantPorts) {
		t.Errorf("ports = %+v, want %+v", web.Ports, wantPorts)
	}
	if web.Image != "nginx:alpine" || web.Restart != "unless-stopped" || !web.Privileged {
		t.Errorf("image = %q, restart = %q, privileged = %v", web.Image, web.Restart, web.Privileged)
	}
	if web.Build != nil {
		t.Error("web has no build section but Build is non-nil")
	}
}
