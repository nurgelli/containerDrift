//go:build integration

// Package integration runs ContainerDrift against a real Docker daemon and the
// real Docker Compose CLI. Run it with: go test -tags integration ./tests/...
// It pulls nginx:alpine and creates, then removes, a temporary Compose project.
package integration

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/nurgelli/containerdrift/internal/compose"
	"github.com/nurgelli/containerdrift/internal/docker"
	"github.com/nurgelli/containerdrift/internal/drift"
)

func composeYAML(hostPort int) string {
	return fmt.Sprintf(`services:
  web:
    image: nginx:alpine
    restart: unless-stopped
    ports:
      - "%d:80"
    volumes:
      - ./data:/data:ro
      - cache:/cache
volumes:
  cache:
`, hostPort)
}

// dockerCompose runs "docker compose" for the test project and fails the test
// when it does not succeed.
func dockerCompose(t *testing.T, file, project string, args ...string) {
	t.Helper()
	full := append([]string{"compose", "-f", file, "-p", project}, args...)
	if out, err := exec.Command("docker", full...).CombinedOutput(); err != nil {
		t.Fatalf("docker %v failed: %v\n%s", full, err, out)
	}
}

func write(t *testing.T, file, content string) {
	t.Helper()
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatalf("writing %s: %v", file, err)
	}
}

// scan runs the same steps as "containerdrift scan".
func scan(t *testing.T, file, project string) drift.ServiceResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	desired, err := compose.NewReader().Read(ctx, compose.Options{Files: []string{file}, ProjectName: project})
	if err != nil {
		t.Fatalf("reading compose configuration: %v", err)
	}
	inspector, err := docker.New()
	if err != nil {
		t.Fatalf("creating inspector: %v", err)
	}
	defer inspector.Close()

	containers, warnings, err := inspector.ListProject(ctx, desired.Name)
	if err != nil {
		t.Fatalf("listing containers: %v", err)
	}
	report := drift.Compare(desired, containers, warnings)
	if len(report.Services) != 1 {
		t.Fatalf("got %d services, want 1: %+v", len(report.Services), report.Services)
	}
	return report.Services[0]
}

func kinds(r drift.ServiceResult) []drift.Kind {
	var out []drift.Kind
	for _, f := range r.Findings {
		out = append(out, f.Kind)
	}
	return out
}

func TestScanAgainstRealDocker(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker is not installed")
	}

	dir := t.TempDir()
	file := filepath.Join(dir, "compose.yaml")
	project := fmt.Sprintf("cdtest%d", time.Now().UnixNano()%1_000_000)
	const portA, portB = 28080, 28081

	write(t, file, composeYAML(portA))
	t.Cleanup(func() {
		// Best effort: remove containers and the named volume.
		_ = exec.Command("docker", "compose", "-f", file, "-p", project, "down", "-v").Run()
	})

	dockerCompose(t, file, project, "up", "-d")

	t.Run("freshly deployed project has no drift", func(t *testing.T) {
		if res := scan(t, file, project); res.Status != drift.StatusPass {
			t.Errorf("status = %q, findings: %+v", res.Status, res.Findings)
		}
	})

	t.Run("changed port in the file is reported", func(t *testing.T) {
		write(t, file, composeYAML(portB))
		res := scan(t, file, project)
		if !slices.Contains(kinds(res), drift.KindPorts) {
			t.Errorf("kinds = %v, want %v", kinds(res), drift.KindPorts)
		}
		write(t, file, composeYAML(portA))
	})

	t.Run("stopped container is reported", func(t *testing.T) {
		dockerCompose(t, file, project, "stop")
		res := scan(t, file, project)
		if !slices.Contains(kinds(res), drift.KindNotRunning) {
			t.Errorf("kinds = %v, want %v", kinds(res), drift.KindNotRunning)
		}
	})

	t.Run("removed container is reported as missing", func(t *testing.T) {
		dockerCompose(t, file, project, "down")
		res := scan(t, file, project)
		if !slices.Contains(kinds(res), drift.KindMissing) {
			t.Errorf("kinds = %v, want %v", kinds(res), drift.KindMissing)
		}
	})
}
