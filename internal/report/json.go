package report

import (
	"encoding/json"
	"io"

	"github.com/nurgelli/containerdrift/internal/drift"
)

// SchemaVersion identifies the JSON layout. It changes only when an existing
// field is removed or changes meaning, so CI scripts can rely on it.
const SchemaVersion = "1"

type jsonReport struct {
	SchemaVersion string        `json:"schemaVersion"`
	Tool          string        `json:"tool"`
	Version       string        `json:"version"`
	Project       string        `json:"project"`
	Summary       jsonSummary   `json:"summary"`
	Services      []jsonService `json:"services"`
	Warnings      []string      `json:"warnings"`
}

type jsonSummary struct {
	Services int `json:"services"`
	Passed   int `json:"passed"`
	Drift    int `json:"drift"`
	Warnings int `json:"warnings"`
}

type jsonService struct {
	Name     string        `json:"name"`
	Status   string        `json:"status"`
	Findings []jsonFinding `json:"findings"`
}

type jsonFinding struct {
	Kind      string   `json:"kind"`
	Severity  string   `json:"severity"`
	Container string   `json:"container,omitempty"`
	Message   string   `json:"message"`
	Expected  []string `json:"expected"`
	Actual    []string `json:"actual"`
}

// WriteJSON writes the machine-readable report. Empty lists are written as []
// rather than null so consumers do not need a nil check.
func WriteJSON(w io.Writer, version string, r drift.Report) error {
	s := r.Summary()
	out := jsonReport{
		SchemaVersion: SchemaVersion,
		Tool:          "containerdrift",
		Version:       version,
		Project:       r.Project,
		Summary:       jsonSummary{Services: s.Services, Passed: s.Passed, Drift: s.Drift, Warnings: s.Warnings},
		Services:      make([]jsonService, 0, len(r.Services)),
		Warnings:      nonNil(r.Warnings),
	}

	for _, svc := range r.Services {
		js := jsonService{Name: svc.Name, Status: string(svc.Status), Findings: make([]jsonFinding, 0, len(svc.Findings))}
		for _, f := range svc.Findings {
			js.Findings = append(js.Findings, jsonFinding{
				Kind:      string(f.Kind),
				Severity:  string(f.Severity),
				Container: f.Container,
				Message:   f.Message,
				Expected:  nonNil(f.Expected),
				Actual:    nonNil(f.Actual),
			})
		}
		out.Services = append(out.Services, js)
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	// Keep "->" readable instead of escaping it as \u003e.
	enc.SetEscapeHTML(false)
	return enc.Encode(out)
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
