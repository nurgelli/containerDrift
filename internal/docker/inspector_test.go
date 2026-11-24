package docker

import (
	"context"
	"errors"
	"net/netip"
	"slices"
	"testing"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"

	"github.com/nurgelli/containerdrift/internal/model"
)

// fakeEngine implements engineAPI without a Docker daemon.
type fakeEngine struct {
	pingErr    error
	list       []container.Summary
	listErr    error
	inspect    map[string]container.InspectResponse
	inspectErr map[string]error

	gotFilters client.Filters
}

func (f *fakeEngine) Ping(context.Context, client.PingOptions) (client.PingResult, error) {
	return client.PingResult{}, f.pingErr
}

func (f *fakeEngine) ContainerList(_ context.Context, opts client.ContainerListOptions) (client.ContainerListResult, error) {
	f.gotFilters = opts.Filters
	if !opts.All {
		return client.ContainerListResult{}, errors.New("test: All must be set so stopped containers are listed")
	}
	return client.ContainerListResult{Items: f.list}, f.listErr
}

func (f *fakeEngine) ContainerInspect(_ context.Context, id string, _ client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
	if err := f.inspectErr[id]; err != nil {
		return client.ContainerInspectResult{}, err
	}
	return client.ContainerInspectResult{Container: f.inspect[id]}, nil
}

// notFoundError satisfies the interface containerd/errdefs looks for.
type notFoundError struct{}

func (notFoundError) Error() string { return "no such container" }
func (notFoundError) NotFound()     {}

func apiResponse(id, name, service, state string) container.InspectResponse {
	return container.InspectResponse{
		ID:   id,
		Name: "/" + name,
		State: &container.State{
			Status: container.ContainerState(state),
		},
		Config: &container.Config{
			Image: "nginx:alpine",
			Labels: map[string]string{
				labelService: service,
				labelOneOff:  "False",
			},
		},
		HostConfig: &container.HostConfig{
			Privileged:    true,
			RestartPolicy: container.RestartPolicy{Name: "on-failure", MaximumRetryCount: 3},
			PortBindings: network.PortMap{
				network.MustParsePort("80/tcp"): {{HostPort: "8080"}},
				network.MustParsePort("90/udp"): {{HostIP: netip.MustParseAddr("127.0.0.1"), HostPort: "9090"}},
			},
		},
		Mounts: []container.MountPoint{
			{Type: mount.TypeVolume, Name: "demo_data", Source: "demo_data", Destination: "/var/data", RW: true},
			{Type: mount.TypeBind, Source: "/srv/conf", Destination: "/etc/app", RW: false},
			{Type: mount.TypeTmpfs, Destination: "/tmp"},
		},
	}
}

func TestListProject(t *testing.T) {
	fake := &fakeEngine{
		list: []container.Summary{{ID: "b2"}, {ID: "a1"}},
		inspect: map[string]container.InspectResponse{
			"a1": apiResponse("a1", "demo-web-1", "web", "running"),
			"b2": apiResponse("b2", "demo-db-1", "db", "exited"),
		},
	}
	insp := &Inspector{api: fake}

	got, warnings, err := insp.ListProject(context.Background(), "demo")
	if err != nil {
		t.Fatalf("ListProject() error = %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v", warnings)
	}
	if !fake.gotFilters["label"]["com.docker.compose.project=demo"] {
		t.Errorf("project label filter missing: %v", fake.gotFilters)
	}

	// Sorted by container name: demo-db-1 first.
	if len(got) != 2 || got[0].Name != "demo-db-1" || got[1].Name != "demo-web-1" {
		t.Fatalf("containers = %+v", got)
	}

	web := got[1]
	if web.Service != "web" || web.State != "running" || web.Image != "nginx:alpine" || web.OneOff || !web.Privileged {
		t.Errorf("web = %+v", web)
	}
	if want := (model.RestartPolicy{Name: "on-failure", MaxRetries: 3}); web.Restart != want {
		t.Errorf("restart = %+v", web.Restart)
	}
	wantPorts := []model.PortBinding{
		{HostPort: "8080", ContainerPort: 80, Protocol: "tcp"},
		{HostIP: "127.0.0.1", HostPort: "9090", ContainerPort: 90, Protocol: "udp"},
	}
	if !slices.Equal(web.Ports, wantPorts) {
		t.Errorf("ports = %+v, want %+v", web.Ports, wantPorts)
	}
	// tmpfs is ignored; mounts are sorted by target.
	wantMounts := []model.Mount{
		{Type: "bind", Source: "/srv/conf", Target: "/etc/app", ReadOnly: true},
		{Type: "volume", Source: "demo_data", Target: "/var/data"},
	}
	if !slices.Equal(web.Mounts, wantMounts) {
		t.Errorf("mounts = %+v, want %+v", web.Mounts, wantMounts)
	}
}

func TestListProjectEmpty(t *testing.T) {
	insp := &Inspector{api: &fakeEngine{}}
	got, warnings, err := insp.ListProject(context.Background(), "demo")
	if err != nil || len(got) != 0 || len(warnings) != 0 {
		t.Errorf("got %v, %v, %v", got, warnings, err)
	}
}

func TestListProjectOneOffAndDefaultRestart(t *testing.T) {
	resp := apiResponse("a1", "demo-web-run-1", "web", "exited")
	resp.Config.Labels[labelOneOff] = "True"
	resp.HostConfig.RestartPolicy = container.RestartPolicy{}

	insp := &Inspector{api: &fakeEngine{
		list:    []container.Summary{{ID: "a1"}},
		inspect: map[string]container.InspectResponse{"a1": resp},
	}}
	got, _, err := insp.ListProject(context.Background(), "demo")
	if err != nil {
		t.Fatalf("ListProject() error = %v", err)
	}
	if !got[0].OneOff {
		t.Error("OneOff = false for label \"True\"")
	}
	if got[0].Restart.Name != "no" {
		t.Errorf("empty restart policy = %q, want \"no\"", got[0].Restart.Name)
	}
}

func TestListProjectContainerDisappears(t *testing.T) {
	insp := &Inspector{api: &fakeEngine{
		list: []container.Summary{{ID: "gone0123456789abc"}, {ID: "a1"}},
		inspect: map[string]container.InspectResponse{
			"a1": apiResponse("a1", "demo-web-1", "web", "running"),
		},
		inspectErr: map[string]error{"gone0123456789abc": notFoundError{}},
	}}

	got, warnings, err := insp.ListProject(context.Background(), "demo")
	if err != nil {
		t.Fatalf("ListProject() error = %v", err)
	}
	if len(got) != 1 || len(warnings) != 1 {
		t.Errorf("got %d containers and %d warnings: %v", len(got), len(warnings), warnings)
	}
}

func TestListProjectMalformedResponse(t *testing.T) {
	broken := container.InspectResponse{ID: "a1", Name: "/x"} // no Config, HostConfig, State
	insp := &Inspector{api: &fakeEngine{
		list:    []container.Summary{{ID: "a1"}},
		inspect: map[string]container.InspectResponse{"a1": broken},
	}}

	got, warnings, err := insp.ListProject(context.Background(), "demo")
	if err != nil {
		t.Fatalf("ListProject() error = %v", err)
	}
	if len(got) != 0 || len(warnings) != 1 {
		t.Errorf("got %d containers, warnings %v", len(got), warnings)
	}
}

func TestListProjectErrors(t *testing.T) {
	boom := errors.New("boom")

	tests := []struct {
		name string
		fake *fakeEngine
	}{
		{"ping fails", &fakeEngine{pingErr: boom}},
		{"list fails", &fakeEngine{listErr: boom}},
		{"inspect fails", &fakeEngine{
			list:       []container.Summary{{ID: "a1"}},
			inspectErr: map[string]error{"a1": boom},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			insp := &Inspector{api: tt.fake}
			_, _, err := insp.ListProject(context.Background(), "demo")
			if !errors.Is(err, boom) {
				t.Errorf("error = %v, want one wrapping boom", err)
			}
		})
	}
}

func TestListProjectCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	insp := &Inspector{api: &fakeEngine{
		list:    []container.Summary{{ID: "a1"}},
		inspect: map[string]container.InspectResponse{"a1": apiResponse("a1", "n", "web", "running")},
	}}
	_, _, err := insp.ListProject(ctx, "demo")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled", err)
	}
}

// TestDaemonUnavailable uses the real client against a closed local port, so
// it needs no Docker but does exercise New-style construction and Ping.
func TestDaemonUnavailable(t *testing.T) {
	c, err := client.New(client.WithHost("tcp://127.0.0.1:1"))
	if err != nil {
		t.Fatalf("client.New() error = %v", err)
	}
	insp := &Inspector{api: c, closeFn: c.Close}
	defer insp.Close()

	_, _, err = insp.ListProject(context.Background(), "demo")
	if !errors.Is(err, ErrUnavailable) {
		t.Errorf("error = %v, want one wrapping ErrUnavailable", err)
	}
}
