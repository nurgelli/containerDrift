// Package docker reads the actual state of a Compose project from the Docker
// Engine API. It only ever lists and inspects containers; it never changes
// anything.
package docker

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"

	"github.com/nurgelli/containerdrift/internal/model"
)

// Labels that Docker Compose attaches to every container it creates.
const (
	labelProject = "com.docker.compose.project"
	labelService = "com.docker.compose.service"
	labelOneOff  = "com.docker.compose.oneoff"
)

// inspectTimeout bounds one whole project listing (ping, list and all inspects).
const inspectTimeout = 30 * time.Second

// ErrUnavailable reports that the Docker daemon cannot be reached.
var ErrUnavailable = errors.New("cannot reach the Docker daemon")

// engineAPI is the small part of the Docker client that Inspector needs.
// *client.Client satisfies it, and tests substitute a fake. The interface is
// defined here, next to its only consumer.
type engineAPI interface {
	Ping(ctx context.Context, opts client.PingOptions) (client.PingResult, error)
	ContainerList(ctx context.Context, opts client.ContainerListOptions) (client.ContainerListResult, error)
	ContainerInspect(ctx context.Context, id string, opts client.ContainerInspectOptions) (client.ContainerInspectResult, error)
}

// Inspector reads containers of a Compose project.
type Inspector struct {
	api     engineAPI
	closeFn func() error
}

// New creates an Inspector that talks to the daemon selected by the usual
// Docker environment variables (DOCKER_HOST and friends). Creating it does not
// connect; the connection is made on first use.
func New() (*Inspector, error) {
	c, err := client.New(client.FromEnv)
	if err != nil {
		return nil, fmt.Errorf("creating Docker client: %w", err)
	}
	return &Inspector{api: c, closeFn: c.Close}, nil
}

// Close releases the underlying client.
func (i *Inspector) Close() error {
	if i.closeFn == nil {
		return nil
	}
	return i.closeFn()
}

// ListProject returns the containers (running or not) that belong to the
// Compose project. The second result lists non-fatal problems, such as a
// container that disappeared while it was being inspected.
func (i *Inspector) ListProject(ctx context.Context, project string) ([]model.ContainerState, []string, error) {
	ctx, cancel := context.WithTimeout(ctx, inspectTimeout)
	defer cancel()

	if _, err := i.api.Ping(ctx, client.PingOptions{}); err != nil {
		if client.IsErrConnectionFailed(err) {
			return nil, nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
		}
		return nil, nil, fmt.Errorf("pinging Docker daemon: %w", err)
	}

	// Filters must be created with a literal: the zero value is a nil map and
	// Add would panic on it.
	filters := client.Filters{}.Add("label", labelProject+"="+project)
	list, err := i.api.ContainerList(ctx, client.ContainerListOptions{All: true, Filters: filters})
	if err != nil {
		return nil, nil, fmt.Errorf("listing containers: %w", err)
	}

	var (
		states   []model.ContainerState
		warnings []string
	)
	for _, summary := range list.Items {
		// A scan can take a moment; stop promptly on Ctrl+C or timeout.
		if err := ctx.Err(); err != nil {
			return nil, nil, fmt.Errorf("inspecting containers: %w", err)
		}

		res, err := i.api.ContainerInspect(ctx, summary.ID, client.ContainerInspectOptions{})
		if err != nil {
			if errdefs.IsNotFound(err) {
				// The container was removed between the list and the inspect.
				warnings = append(warnings, fmt.Sprintf("container %s disappeared during the scan and was skipped", shortID(summary.ID)))
				continue
			}
			return nil, nil, fmt.Errorf("inspecting container %s: %w", shortID(summary.ID), err)
		}

		state, ok := convertInspect(res.Container)
		if !ok {
			warnings = append(warnings, fmt.Sprintf("container %s has an incomplete Docker API response and was skipped", shortID(summary.ID)))
			continue
		}
		states = append(states, state)
	}

	slices.SortFunc(states, func(a, b model.ContainerState) int { return strings.Compare(a.Name, b.Name) })
	return states, warnings, nil
}

// convertInspect maps an inspect response to the application model. It
// returns false when the response lacks the sections we depend on, so a
// malformed response is reported instead of causing a nil dereference.
func convertInspect(r container.InspectResponse) (model.ContainerState, bool) {
	if r.Config == nil || r.HostConfig == nil || r.State == nil {
		return model.ContainerState{}, false
	}

	state := model.ContainerState{
		ID:      r.ID,
		Name:    strings.TrimPrefix(r.Name, "/"),
		Service: r.Config.Labels[labelService],
		State:   string(r.State.Status),
		// Compose writes "True" and "False" with a capital letter.
		OneOff:     strings.EqualFold(r.Config.Labels[labelOneOff], "true"),
		Image:      r.Config.Image,
		Ports:      convertPorts(r.HostConfig.PortBindings),
		Mounts:     convertMounts(r.Mounts),
		Privileged: r.HostConfig.Privileged,
		Restart: model.RestartPolicy{
			Name:       string(r.HostConfig.RestartPolicy.Name),
			MaxRetries: r.HostConfig.RestartPolicy.MaximumRetryCount,
		},
	}
	if state.Restart.Name == "" {
		state.Restart.Name = "no"
	}
	return state, true
}

func convertPorts(bindings network.PortMap) []model.PortBinding {
	var ports []model.PortBinding
	for port, list := range bindings {
		for _, b := range list {
			pb := model.PortBinding{
				HostPort:      b.HostPort,
				ContainerPort: int(port.Num()),
				Protocol:      string(port.Proto()),
			}
			// The zero netip.Addr means "no host IP given"; its String()
			// would print "invalid IP", so check validity first.
			if b.HostIP.IsValid() {
				pb.HostIP = b.HostIP.String()
			}
			ports = append(ports, pb)
		}
	}
	// Map iteration order is random; sort for deterministic output.
	slices.SortFunc(ports, func(a, b model.PortBinding) int {
		return cmp.Or(
			cmp.Compare(a.ContainerPort, b.ContainerPort),
			strings.Compare(a.Protocol, b.Protocol),
			strings.Compare(a.HostIP, b.HostIP),
			strings.Compare(a.HostPort, b.HostPort),
		)
	})
	return ports
}

func convertMounts(points []container.MountPoint) []model.Mount {
	var mounts []model.Mount
	for _, p := range points {
		switch p.Type {
		case mount.TypeBind:
			mounts = append(mounts, model.Mount{Type: "bind", Source: p.Source, Target: p.Destination, ReadOnly: !p.RW})
		case mount.TypeVolume:
			mounts = append(mounts, model.Mount{Type: "volume", Source: p.Name, Target: p.Destination, ReadOnly: !p.RW})
		}
	}
	slices.SortFunc(mounts, func(a, b model.Mount) int { return strings.Compare(a.Target, b.Target) })
	return mounts
}

func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}
