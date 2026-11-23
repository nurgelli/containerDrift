package compose

import (
	"os"
	"slices"
	"testing"

	"github.com/nurgelli/containerdrift/internal/model"
)

func convertFixture(t *testing.T, name string) model.Project {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	raw, err := decodeConfig(data)
	if err != nil {
		t.Fatalf("decodeConfig() error = %v", err)
	}
	project, err := convert(raw)
	if err != nil {
		t.Fatalf("convert() error = %v", err)
	}
	return project
}

func TestConvertEdgeCases(t *testing.T) {
	project := convertFixture(t, "config_edge.json")
	if len(project.Services) != 1 {
		t.Fatalf("got %d services, want 1", len(project.Services))
	}
	web := project.Services[0]

	wantPorts := []model.PortBinding{
		{ContainerPort: 3000, Protocol: "tcp"},
		{HostPort: "8000", ContainerPort: 8000, Protocol: "tcp"},
		{HostPort: "8001", ContainerPort: 8001, Protocol: "tcp"},
		{HostPort: "8002", ContainerPort: 8002, Protocol: "tcp"},
		{HostIP: "::1", HostPort: "7070", ContainerPort: 70, Protocol: "tcp"},
	}
	if !slices.Equal(web.Ports, wantPorts) {
		t.Errorf("ports = %+v, want %+v", web.Ports, wantPorts)
	}

	wantMounts := []model.Mount{
		{Type: "bind", Source: `C:\Users\demo\project\data`, Target: "/data", ReadOnly: true},
		{Type: "volume", Source: "containerdrift_cache", Target: "/cache"},
	}
	if !slices.Equal(web.Mounts, wantMounts) {
		t.Errorf("mounts = %+v, want %+v", web.Mounts, wantMounts)
	}

	if want := (model.RestartPolicy{Name: "on-failure", MaxRetries: 3}); web.Restart != want {
		t.Errorf("restart = %+v, want %+v", web.Restart, want)
	}
}

func TestConvertRealWebOutput(t *testing.T) {
	web := convertFixture(t, "config_web.json").Services[0]

	wantPorts := []model.PortBinding{
		{HostPort: "8080", ContainerPort: 80, Protocol: "tcp"},
		{HostIP: "127.0.0.1", HostPort: "9090", ContainerPort: 90, Protocol: "udp"},
	}
	if !slices.Equal(web.Ports, wantPorts) {
		t.Errorf("ports = %+v, want %+v", web.Ports, wantPorts)
	}
	if want := (model.RestartPolicy{Name: "unless-stopped"}); web.Restart != want {
		t.Errorf("restart = %+v, want %+v", web.Restart, want)
	}
	if !web.Privileged || web.HasBuild || web.Image != "nginx:alpine" {
		t.Errorf("privileged = %v, hasBuild = %v, image = %q", web.Privileged, web.HasBuild, web.Image)
	}
}

func TestConvertSortsServicesAndDefaults(t *testing.T) {
	project := convertFixture(t, "config_full.json")

	if len(project.Services) != 2 || project.Services[0].Name != "api" || project.Services[1].Name != "db" {
		t.Fatalf("services not sorted by name: %+v", project.Services)
	}
	if !project.Services[0].HasBuild {
		t.Error("api should have HasBuild = true")
	}
	if want := (model.RestartPolicy{Name: "no"}); project.Services[0].Restart != want {
		t.Errorf("unset restart = %+v, want %+v", project.Services[0].Restart, want)
	}
	if got := project.Services[1].Mounts[0].Source; got != "demo_db_data" {
		t.Errorf("named volume source = %q, want resolved name demo_db_data", got)
	}
}

func TestParseRestart(t *testing.T) {
	tests := []struct {
		in      string
		want    model.RestartPolicy
		wantErr bool
	}{
		{"", model.RestartPolicy{Name: "no"}, false},
		{"no", model.RestartPolicy{Name: "no"}, false},
		{"always", model.RestartPolicy{Name: "always"}, false},
		{"unless-stopped", model.RestartPolicy{Name: "unless-stopped"}, false},
		{"on-failure", model.RestartPolicy{Name: "on-failure"}, false},
		{"on-failure:3", model.RestartPolicy{Name: "on-failure", MaxRetries: 3}, false},
		{"on-failure:x", model.RestartPolicy{}, true},
		{"on-failure:-1", model.RestartPolicy{}, true},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := parseRestart(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseRestart(%q) error = %v, wantErr %v", tt.in, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("parseRestart(%q) = %+v, want %+v", tt.in, got, tt.want)
			}
		})
	}
}

func TestConvertWarnsAboutUnsupportedMount(t *testing.T) {
	raw := rawProject{
		Name: "demo",
		Services: map[string]rawService{
			"web": {Image: "nginx", Volumes: []rawMount{{Type: "tmpfs", Target: "/tmp"}}},
		},
	}

	project, err := convert(raw)
	if err != nil {
		t.Fatalf("convert() error = %v", err)
	}
	if len(project.Warnings) != 1 {
		t.Errorf("got %d warnings, want 1: %v", len(project.Warnings), project.Warnings)
	}
	if len(project.Services[0].Mounts) != 0 {
		t.Errorf("unsupported mount was not skipped: %+v", project.Services[0].Mounts)
	}
}

func TestFirstLines(t *testing.T) {
	tests := []struct {
		name string
		in   string
		n    int
		want string
	}{
		{"short", "one\ntwo", 5, "one\ntwo"},
		{"truncated", "1\n2\n3\n4", 2, "1\n2\n..."},
		{"windows newlines", "a\r\nb\r\nc", 2, "a\nb\n..."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := firstLines(tt.in, tt.n); got != tt.want {
				t.Errorf("firstLines() = %q, want %q", got, tt.want)
			}
		})
	}
}
