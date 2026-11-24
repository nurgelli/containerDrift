// Package drift compares the desired state (Compose) with the actual state
// (Docker) and describes the differences. It performs no I/O: every function
// is a pure function of its arguments, which makes it easy to test.
package drift

import (
	"fmt"
	"slices"
	"strings"

	"github.com/nurgelli/containerdrift/internal/model"
)

// Kind identifies the type of a finding. The values are part of the JSON output.
type Kind string

const (
	KindMissing    Kind = "missing_container"
	KindNotRunning Kind = "container_not_running"
	KindUnexpected Kind = "unexpected_container"
	KindImage      Kind = "image_mismatch"
	KindPorts      Kind = "port_mismatch"
	KindVolumes    Kind = "volume_mismatch"
	KindRestart    Kind = "restart_policy_mismatch"
	KindPrivileged Kind = "privileged_mismatch"
)

// Severity separates confirmed drift from observations that need a human.
type Severity string

const (
	SeverityDrift   Severity = "drift"
	SeverityWarning Severity = "warning"
)

// Status is the outcome for one service.
type Status string

const (
	StatusPass    Status = "pass"
	StatusDrift   Status = "drift"
	StatusWarning Status = "warning"
)

// Finding is one detected difference.
type Finding struct {
	Kind      Kind
	Severity  Severity
	Container string // container name, empty for service-level findings
	Message   string
	Expected  []string
	Actual    []string
}

// ServiceResult is the outcome for one service.
type ServiceResult struct {
	Name     string
	Status   Status
	Matches  []string // checks that were performed and passed, e.g. "Image matches"
	Findings []Finding
}

// Report is the result of comparing a whole project.
type Report struct {
	Project  string
	Services []ServiceResult // desired services first, then unexpected ones, each sorted
	Warnings []string        // problems that are not drift (skipped features, vanished containers)
}

// Summary counts services by status.
type Summary struct {
	Services int
	Passed   int
	Drift    int
	Warnings int
}

// Summary tallies the report.
func (r Report) Summary() Summary {
	s := Summary{Services: len(r.Services)}
	for _, svc := range r.Services {
		switch svc.Status {
		case StatusPass:
			s.Passed++
		case StatusDrift:
			s.Drift++
		case StatusWarning:
			s.Warnings++
		}
	}
	return s
}

// Compare builds the drift report. Warnings carries non-fatal problems found
// while collecting the two states; they are copied into the report.
func Compare(desired model.Project, actual []model.ContainerState, warnings []string) Report {
	report := Report{Project: desired.Name}
	report.Warnings = append(report.Warnings, desired.Warnings...)
	report.Warnings = append(report.Warnings, warnings...)

	// Group containers by service. One-off containers ("docker compose run")
	// are not part of the declared configuration and are ignored.
	byService := make(map[string][]model.ContainerState)
	for _, c := range actual {
		if c.OneOff {
			continue
		}
		byService[c.Service] = append(byService[c.Service], c)
	}

	known := make(map[string]bool, len(desired.Services))
	for _, spec := range desired.Services {
		known[spec.Name] = true
		report.Services = append(report.Services, compareService(spec, byService[spec.Name]))
	}

	var extra []string
	for name := range byService {
		if !known[name] {
			extra = append(extra, name)
		}
	}
	slices.Sort(extra)
	for _, name := range extra {
		report.Services = append(report.Services, unexpectedService(name, byService[name]))
	}
	return report
}

func unexpectedService(name string, containers []model.ContainerState) ServiceResult {
	res := ServiceResult{Name: name}
	for _, c := range containers {
		res.Findings = append(res.Findings, Finding{
			Kind:      KindUnexpected,
			Severity:  SeverityDrift,
			Container: c.Name,
			Message: fmt.Sprintf("Container %s belongs to service %q, which is not in the Compose configuration "+
				"(removed service, or a profile that was not enabled with --profile)", c.Name, name),
			Actual: []string{fmt.Sprintf("%s (%s)", c.Name, c.State)},
		})
	}
	res.Status = statusOf(res.Findings)
	return res
}

func compareService(spec model.ServiceSpec, containers []model.ContainerState) ServiceResult {
	res := ServiceResult{Name: spec.Name}

	if len(containers) == 0 {
		res.Findings = append(res.Findings, Finding{
			Kind:     KindMissing,
			Severity: SeverityDrift,
			Message:  "No container exists for this service",
			Expected: []string{"at least one container"},
			Actual:   []string{"none"},
		})
		res.Status = statusOf(res.Findings)
		return res
	}

	// With several containers (scaled services) every container is compared
	// against the same spec; a check passes only if it passes for all of them.
	checks := []struct {
		name string
		run  func(model.ServiceSpec, model.ContainerState) []Finding
		// skip reports why a check was not performed, if it was not.
		skip func(model.ServiceSpec) bool
		ok   string
	}{
		{"image", compareImage, func(s model.ServiceSpec) bool { return s.Image == "" }, "Image matches"},
		{"ports", comparePorts, nil, "Port mapping matches"},
		{"volumes", compareMounts, nil, "Volume mounts match"},
		{"restart", compareRestart, nil, "Restart policy matches"},
		{"privileged", comparePrivileged, nil, "Privileged mode matches"},
	}

	running := 0
	for _, c := range containers {
		if c.State == "running" {
			running++
			continue
		}
		res.Findings = append(res.Findings, Finding{
			Kind:      KindNotRunning,
			Severity:  SeverityDrift,
			Container: c.Name,
			Message:   fmt.Sprintf("Container %s is not running", c.Name),
			Expected:  []string{"running"},
			Actual:    []string{c.State},
		})
	}

	for _, check := range checks {
		if check.skip != nil && check.skip(spec) {
			continue
		}
		var found []Finding
		for _, c := range containers {
			found = append(found, check.run(spec, c)...)
		}
		if len(found) == 0 {
			res.Matches = append(res.Matches, check.ok)
		}
		res.Findings = append(res.Findings, found...)
	}

	res.Status = statusOf(res.Findings)
	return res
}

func statusOf(findings []Finding) Status {
	status := StatusPass
	for _, f := range findings {
		switch f.Severity {
		case SeverityDrift:
			return StatusDrift
		case SeverityWarning:
			status = StatusWarning
		}
	}
	return status
}

// labelled prefixes a message with the container name when it is useful.
func labelled(c model.ContainerState, msg string) string {
	return fmt.Sprintf("%s (container %s)", msg, c.Name)
}

func compareImage(spec model.ServiceSpec, c model.ContainerState) []Finding {
	if normalizeImage(spec.Image) == normalizeImage(c.Image) {
		return nil
	}
	return []Finding{{
		Kind:      KindImage,
		Severity:  SeverityDrift,
		Container: c.Name,
		Message:   labelled(c, "Image mismatch"),
		Expected:  []string{spec.Image},
		Actual:    []string{c.Image},
	}}
}

// normalizeImage makes equivalent spellings of an image reference comparable:
// the Docker Hub registry prefixes are dropped and an untagged reference gets
// the implicit ":latest" tag.
func normalizeImage(ref string) string {
	for _, prefix := range []string{"docker.io/library/", "index.docker.io/library/", "docker.io/", "index.docker.io/"} {
		if rest, ok := strings.CutPrefix(ref, prefix); ok {
			ref = rest
			break
		}
	}
	if strings.Contains(ref, "@") {
		return ref // digest-pinned references have no tag
	}
	// A colon after the last slash is a tag separator; before it, a registry port.
	lastSlash := strings.LastIndex(ref, "/")
	if !strings.Contains(ref[lastSlash+1:], ":") {
		ref += ":latest"
	}
	return ref
}

func compareRestart(spec model.ServiceSpec, c model.ContainerState) []Finding {
	if spec.Restart == c.Restart {
		return nil
	}
	return []Finding{{
		Kind:      KindRestart,
		Severity:  SeverityDrift,
		Container: c.Name,
		Message:   labelled(c, "Restart policy mismatch"),
		Expected:  []string{formatRestart(spec.Restart)},
		Actual:    []string{formatRestart(c.Restart)},
	}}
}

func formatRestart(r model.RestartPolicy) string {
	if r.Name == "on-failure" && r.MaxRetries > 0 {
		return fmt.Sprintf("on-failure:%d", r.MaxRetries)
	}
	return r.Name
}

func comparePrivileged(spec model.ServiceSpec, c model.ContainerState) []Finding {
	if spec.Privileged == c.Privileged {
		return nil
	}
	return []Finding{{
		Kind:      KindPrivileged,
		Severity:  SeverityDrift,
		Container: c.Name,
		Message:   labelled(c, "Privileged mode mismatch"),
		Expected:  []string{fmt.Sprint(spec.Privileged)},
		Actual:    []string{fmt.Sprint(c.Privileged)},
	}}
}
