package compose

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/nurgelli/containerdrift/internal/model"
)

// Read returns the desired state described by the Compose configuration.
func (r *Reader) Read(ctx context.Context, opts Options) (model.Project, error) {
	raw, err := r.loadRaw(ctx, opts)
	if err != nil {
		return model.Project{}, err
	}
	return convert(raw)
}

func convert(raw rawProject) (model.Project, error) {
	project := model.Project{Name: raw.Name}

	// Map iteration order is random in Go; sort so output is deterministic.
	for _, name := range slices.Sorted(maps.Keys(raw.Services)) {
		spec, warnings, err := convertService(name, raw.Services[name], raw.Volumes)
		if err != nil {
			return model.Project{}, fmt.Errorf("service %q: %w", name, err)
		}
		project.Services = append(project.Services, spec)
		project.Warnings = append(project.Warnings, warnings...)
	}
	return project, nil
}

func convertService(name string, s rawService, volumes map[string]rawVolume) (model.ServiceSpec, []string, error) {
	restart, err := parseRestart(s.Restart)
	if err != nil {
		return model.ServiceSpec{}, nil, err
	}

	spec := model.ServiceSpec{
		Name:       name,
		Image:      s.Image,
		HasBuild:   s.Build != nil,
		Restart:    restart,
		Privileged: s.Privileged,
	}

	for _, p := range s.Ports {
		spec.Ports = append(spec.Ports, convertPort(p))
	}

	var warnings []string
	for _, m := range s.Volumes {
		mount, ok := convertMount(m, volumes)
		if !ok {
			warnings = append(warnings,
				fmt.Sprintf("service %q: mount type %q is not supported and was skipped", name, m.Type))
			continue
		}
		spec.Mounts = append(spec.Mounts, mount)
	}
	return spec, warnings, nil
}

func convertPort(p rawPort) model.PortBinding {
	protocol := p.Protocol
	if protocol == "" {
		protocol = "tcp"
	}
	return model.PortBinding{
		HostIP:        p.HostIP,
		HostPort:      p.Published,
		ContainerPort: p.Target,
		Protocol:      protocol,
	}
}

func convertMount(m rawMount, volumes map[string]rawVolume) (model.Mount, bool) {
	switch m.Type {
	case "bind":
		return model.Mount{Type: "bind", Source: m.Source, Target: m.Target, ReadOnly: m.ReadOnly}, true
	case "volume":
		source := m.Source
		// Compose reports the short name here; the top-level volumes section
		// holds the resolved name Docker actually uses.
		if v, ok := volumes[source]; ok && v.Name != "" {
			source = v.Name
		}
		return model.Mount{Type: "volume", Source: source, Target: m.Target, ReadOnly: m.ReadOnly}, true
	default:
		return model.Mount{}, false
	}
}

// parseRestart converts Compose's "name" or "name:retries" into a policy.
// An unset policy is "no", which is what Docker reports for it.
func parseRestart(s string) (model.RestartPolicy, error) {
	if s == "" {
		return model.RestartPolicy{Name: "no"}, nil
	}

	name, retries, hasRetries := strings.Cut(s, ":")
	policy := model.RestartPolicy{Name: name}
	if hasRetries {
		n, err := strconv.Atoi(retries)
		if err != nil || n < 0 {
			return model.RestartPolicy{}, fmt.Errorf("invalid restart policy %q", s)
		}
		policy.MaxRetries = n
	}
	return policy, nil
}
