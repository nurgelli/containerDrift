package drift

import (
	"fmt"
	"slices"
	"strings"

	"github.com/nurgelli/containerdrift/internal/model"
)

// comparePorts matches desired port bindings against actual ones.
//
// A desired binding matches an actual one when container port, protocol and
// host port are equal. An empty desired host port means "any host port" (the
// Docker daemon picks one), and an empty desired host IP means "any host IP",
// because Compose then does not pin the address. Actual bindings that no
// desired binding explains are reported as unexpected.
func comparePorts(spec model.ServiceSpec, c model.ContainerState) []Finding {
	var (
		missing []model.PortBinding
		used    = make([]bool, len(c.Ports))
	)

	for _, want := range spec.Ports {
		// Each actual binding can explain at most one desired binding.
		idx := -1
		for i, got := range c.Ports {
			if !used[i] && portMatches(want, got) {
				idx = i
				break
			}
		}
		if idx < 0 {
			missing = append(missing, want)
			continue
		}
		used[idx] = true
	}

	var extra []model.PortBinding
	for i, got := range c.Ports {
		if used[i] {
			continue
		}
		// Docker may report one published port twice (IPv4 and IPv6). If a
		// desired binding that does not pin the host IP already explains the
		// port, the second entry is not drift.
		if slices.ContainsFunc(spec.Ports, func(want model.PortBinding) bool {
			return want.HostIP == "" && portMatches(want, got)
		}) {
			continue
		}
		extra = append(extra, got)
	}

	if len(missing) == 0 && len(extra) == 0 {
		return nil
	}

	return []Finding{{
		Kind:      KindPorts,
		Severity:  SeverityDrift,
		Container: c.Name,
		Message:   labelled(c, "Port mapping mismatch"),
		Expected:  formatPorts(spec.Ports),
		Actual:    formatPorts(c.Ports),
	}}
}

func portMatches(want, got model.PortBinding) bool {
	if want.ContainerPort != got.ContainerPort || want.Protocol != got.Protocol {
		return false
	}
	if want.HostPort != "" && want.HostPort != got.HostPort {
		return false
	}
	if want.HostIP != "" && want.HostIP != got.HostIP {
		return false
	}
	return true
}

func formatPorts(ports []model.PortBinding) []string {
	if len(ports) == 0 {
		return []string{"(none)"}
	}
	out := make([]string, 0, len(ports))
	for _, p := range ports {
		out = append(out, formatPort(p))
	}
	slices.Sort(out)
	return out
}

func formatPort(p model.PortBinding) string {
	host := p.HostIP
	if host == "" {
		host = "0.0.0.0"
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]" // IPv6
	}
	hostPort := p.HostPort
	if hostPort == "" {
		hostPort = "(random)"
	}
	return fmt.Sprintf("%s:%s -> %d/%s", host, hostPort, p.ContainerPort, p.Protocol)
}
