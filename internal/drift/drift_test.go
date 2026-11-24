package drift

import (
	"slices"
	"testing"

	"github.com/nurgelli/containerdrift/internal/model"
)

// goodContainer returns a container that matches goodSpec exactly.
func goodSpec() model.ServiceSpec {
	return model.ServiceSpec{
		Name:    "web",
		Image:   "nginx:alpine",
		Ports:   []model.PortBinding{{HostPort: "8080", ContainerPort: 80, Protocol: "tcp"}},
		Mounts:  []model.Mount{{Type: "bind", Source: "/srv/data", Target: "/data"}},
		Restart: model.RestartPolicy{Name: "unless-stopped"},
	}
}

func goodContainer() model.ContainerState {
	return model.ContainerState{
		Name:    "demo-web-1",
		Service: "web",
		State:   "running",
		Image:   "nginx:alpine",
		Ports:   []model.PortBinding{{HostPort: "8080", ContainerPort: 80, Protocol: "tcp"}},
		Mounts: []model.Mount{
			{Type: "bind", Source: "/srv/data", Target: "/data"},
			{Type: "volume", Source: "anonymous-from-image", Target: "/image-volume"},
		},
		Restart: model.RestartPolicy{Name: "unless-stopped"},
	}
}

func kinds(r ServiceResult) []Kind {
	var out []Kind
	for _, f := range r.Findings {
		out = append(out, f.Kind)
	}
	return out
}

func TestCompareService(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*model.ServiceSpec, *model.ContainerState)
		wantKinds []Kind
		want      Status
	}{
		{"everything matches", func(*model.ServiceSpec, *model.ContainerState) {}, nil, StatusPass},
		{"stopped container", func(_ *model.ServiceSpec, c *model.ContainerState) { c.State = "exited" },
			[]Kind{KindNotRunning}, StatusDrift},
		{"image differs", func(_ *model.ServiceSpec, c *model.ContainerState) { c.Image = "nginx:1.25" },
			[]Kind{KindImage}, StatusDrift},
		{"implicit latest tag", func(s *model.ServiceSpec, c *model.ContainerState) {
			s.Image = "nginx"
			c.Image = "docker.io/library/nginx:latest"
		},
			nil, StatusPass},
		{"port differs", func(_ *model.ServiceSpec, c *model.ContainerState) { c.Ports[0].HostPort = "8081" },
			[]Kind{KindPorts}, StatusDrift},
		{"port missing", func(_ *model.ServiceSpec, c *model.ContainerState) { c.Ports = nil },
			[]Kind{KindPorts}, StatusDrift},
		{"extra port", func(_ *model.ServiceSpec, c *model.ContainerState) {
			c.Ports = append(c.Ports, model.PortBinding{HostPort: "9999", ContainerPort: 99, Protocol: "tcp"})
		}, []Kind{KindPorts}, StatusDrift},
		{"random host port accepts any", func(s *model.ServiceSpec, c *model.ContainerState) {
			s.Ports[0].HostPort = ""
			c.Ports[0].HostPort = "49153"
		}, nil, StatusPass},
		{"unpinned host ip accepts any, including dual stack", func(_ *model.ServiceSpec, c *model.ContainerState) {
			c.Ports = []model.PortBinding{
				{HostIP: "0.0.0.0", HostPort: "8080", ContainerPort: 80, Protocol: "tcp"},
				{HostIP: "::", HostPort: "8080", ContainerPort: 80, Protocol: "tcp"},
			}
		}, nil, StatusPass},
		{"pinned host ip must match", func(s *model.ServiceSpec, c *model.ContainerState) {
			s.Ports[0].HostIP = "127.0.0.1"
			c.Ports[0].HostIP = "0.0.0.0"
		}, []Kind{KindPorts}, StatusDrift},
		{"protocol differs", func(_ *model.ServiceSpec, c *model.ContainerState) { c.Ports[0].Protocol = "udp" },
			[]Kind{KindPorts}, StatusDrift},
		{"volume source differs", func(_ *model.ServiceSpec, c *model.ContainerState) { c.Mounts[0].Source = "/other" },
			[]Kind{KindVolumes}, StatusDrift},
		{"volume missing", func(_ *model.ServiceSpec, c *model.ContainerState) { c.Mounts = nil },
			[]Kind{KindVolumes}, StatusDrift},
		{"read-only differs", func(_ *model.ServiceSpec, c *model.ContainerState) { c.Mounts[0].ReadOnly = true },
			[]Kind{KindVolumes}, StatusDrift},
		{"restart differs", func(_ *model.ServiceSpec, c *model.ContainerState) { c.Restart = model.RestartPolicy{Name: "always"} },
			[]Kind{KindRestart}, StatusDrift},
		{"privileged differs", func(_ *model.ServiceSpec, c *model.ContainerState) { c.Privileged = true },
			[]Kind{KindPrivileged}, StatusDrift},
		{"built service skips the image check", func(s *model.ServiceSpec, c *model.ContainerState) {
			s.Image = ""
			s.HasBuild = true
			c.Image = "demo-web"
		}, nil, StatusPass},
		{"several problems at once", func(_ *model.ServiceSpec, c *model.ContainerState) {
			c.Image = "nginx:1.25"
			c.Privileged = true
		}, []Kind{KindImage, KindPrivileged}, StatusDrift},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec, c := goodSpec(), goodContainer()
			tt.mutate(&spec, &c)

			got := compareService(spec, []model.ContainerState{c})
			if !slices.Equal(kinds(got), tt.wantKinds) {
				t.Errorf("kinds = %v, want %v (findings: %+v)", kinds(got), tt.wantKinds, got.Findings)
			}
			if got.Status != tt.want {
				t.Errorf("status = %q, want %q", got.Status, tt.want)
			}
		})
	}
}

func TestCompareMatchesList(t *testing.T) {
	got := compareService(goodSpec(), []model.ContainerState{goodContainer()})
	want := []string{"Image matches", "Port mapping matches", "Volume mounts match", "Restart policy matches", "Privileged mode matches"}
	if !slices.Equal(got.Matches, want) {
		t.Errorf("matches = %v, want %v", got.Matches, want)
	}

	// A failing check must not be listed as a match.
	c := goodContainer()
	c.Image = "other"
	got = compareService(goodSpec(), []model.ContainerState{c})
	if slices.Contains(got.Matches, "Image matches") {
		t.Errorf("failed check listed as a match: %v", got.Matches)
	}
}

func TestCompareProject(t *testing.T) {
	desired := model.Project{
		Name:     "demo",
		Services: []model.ServiceSpec{goodSpec(), {Name: "db", Image: "postgres:15", Restart: model.RestartPolicy{Name: "no"}}},
		Warnings: []string{"compose warning"},
	}
	oneOff := goodContainer()
	oneOff.Name = "demo-web-run-1"
	oneOff.OneOff = true
	oneOff.Image = "something-else" // must be ignored

	orphan := model.ContainerState{Name: "demo-old-1", Service: "old", State: "running"}

	report := Compare(desired, []model.ContainerState{goodContainer(), oneOff, orphan}, []string{"vanished"})

	if report.Project != "demo" {
		t.Errorf("project = %q", report.Project)
	}
	if !slices.Equal(report.Warnings, []string{"compose warning", "vanished"}) {
		t.Errorf("warnings = %v", report.Warnings)
	}

	gotNames := []string{}
	for _, s := range report.Services {
		gotNames = append(gotNames, s.Name)
	}
	if !slices.Equal(gotNames, []string{"web", "db", "old"}) {
		t.Fatalf("service order = %v", gotNames)
	}

	web, db, old := report.Services[0], report.Services[1], report.Services[2]
	if web.Status != StatusPass {
		t.Errorf("web = %+v (one-off container must be ignored)", web)
	}
	if db.Status != StatusDrift || kinds(db)[0] != KindMissing {
		t.Errorf("db = %+v", db)
	}
	if old.Status != StatusDrift || kinds(old)[0] != KindUnexpected {
		t.Errorf("old = %+v", old)
	}

	if got, want := report.Summary(), (Summary{Services: 3, Passed: 1, Drift: 2}); got != want {
		t.Errorf("summary = %+v, want %+v", got, want)
	}
}

func TestCompareEmptyProject(t *testing.T) {
	report := Compare(model.Project{Name: "empty"}, nil, nil)
	if len(report.Services) != 0 || report.Summary() != (Summary{}) {
		t.Errorf("report = %+v", report)
	}
}

func TestScaledService(t *testing.T) {
	a, b := goodContainer(), goodContainer()
	a.Name, b.Name = "demo-web-1", "demo-web-2"
	b.Image = "nginx:1.25" // only the second replica drifted

	got := compareService(goodSpec(), []model.ContainerState{a, b})
	if len(got.Findings) != 1 || got.Findings[0].Container != "demo-web-2" {
		t.Errorf("findings = %+v", got.Findings)
	}
}

func TestNormalizeImage(t *testing.T) {
	tests := map[string]string{
		"nginx":                          "nginx:latest",
		"nginx:alpine":                   "nginx:alpine",
		"docker.io/library/nginx:alpine": "nginx:alpine",
		"docker.io/bitnami/redis":        "bitnami/redis:latest",
		"localhost:5000/app":             "localhost:5000/app:latest",
		"localhost:5000/app:v2":          "localhost:5000/app:v2",
		"ghcr.io/org/app@sha256:abc":     "ghcr.io/org/app@sha256:abc",
	}
	for in, want := range tests {
		t.Run(in, func(t *testing.T) {
			if got := normalizeImage(in); got != want {
				t.Errorf("normalizeImage(%q) = %q, want %q", in, got, want)
			}
		})
	}
}

func TestSameHostPath(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		{"/srv/data", "/srv/data", true},
		{"/srv/data/", "/srv/data", true},
		{"/srv/Data", "/srv/data", false}, // Linux paths are case-sensitive
		{`C:\Users\me\data`, `c:\users\me\data`, true},
		{`C:\Users\me\data`, `C:/Users/me/data`, true},
		{`C:\Users\me\data`, `C:\Users\me\logs`, false},
	}
	for _, tt := range tests {
		if got := sameHostPath(tt.a, tt.b); got != tt.want {
			t.Errorf("sameHostPath(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestFormatPort(t *testing.T) {
	tests := []struct {
		in   model.PortBinding
		want string
	}{
		{model.PortBinding{HostPort: "8080", ContainerPort: 80, Protocol: "tcp"}, "0.0.0.0:8080 -> 80/tcp"},
		{model.PortBinding{HostIP: "::1", HostPort: "7070", ContainerPort: 70, Protocol: "tcp"}, "[::1]:7070 -> 70/tcp"},
		{model.PortBinding{ContainerPort: 3000, Protocol: "tcp"}, "0.0.0.0:(random) -> 3000/tcp"},
	}
	for _, tt := range tests {
		if got := formatPort(tt.in); got != tt.want {
			t.Errorf("formatPort() = %q, want %q", got, tt.want)
		}
	}
}
