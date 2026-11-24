package report

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/nurgelli/containerdrift/internal/drift"
)

func sampleReport() drift.Report {
	return drift.Report{
		Project: "my-backend",
		Services: []drift.ServiceResult{
			{Name: "postgres", Status: drift.StatusPass, Matches: []string{"Image matches", "Port mapping matches"}},
			{Name: "api", Status: drift.StatusDrift, Findings: []drift.Finding{{
				Kind:     drift.KindPorts,
				Severity: drift.SeverityDrift,
				Message:  "Port mapping mismatch",
				Expected: []string{"0.0.0.0:8080 -> 8080/tcp"},
				Actual:   []string{"0.0.0.0:8081 -> 8080/tcp"},
			}}},
		},
		Warnings: []string{"something was skipped"},
	}
}

func TestWriteTable(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteTable(&buf, "0.1.0", sampleReport()); err != nil {
		t.Fatalf("WriteTable() error = %v", err)
	}

	want := `ContainerDrift v0.1.0

Project: my-backend

[PASS] postgres
  Image matches
  Port mapping matches

[DRIFT] api
  Port mapping mismatch

  Expected:
    0.0.0.0:8080 -> 8080/tcp

  Actual:
    0.0.0.0:8081 -> 8080/tcp

Warnings:
  - something was skipped

Summary:
  Services: 2
  Passed: 1
  Drift detected: 1
  Warnings: 0

Scan completed.
`
	if got := buf.String(); got != want {
		t.Errorf("output differs.\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestWriteTableHidesMatchesOfDriftingService(t *testing.T) {
	r := drift.Report{Project: "p", Services: []drift.ServiceResult{{
		Name: "api", Status: drift.StatusDrift, Matches: []string{"Image matches"},
		Findings: []drift.Finding{{Message: "Restart policy mismatch"}},
	}}}
	var buf bytes.Buffer
	if err := WriteTable(&buf, "dev", r); err != nil {
		t.Fatalf("WriteTable() error = %v", err)
	}
	if strings.Contains(buf.String(), "Image matches") {
		t.Errorf("a drifting service must not list matches:\n%s", buf.String())
	}
}

func TestWriteJSONDoesNotEscapeArrows(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteJSON(&buf, "dev", sampleReport()); err != nil {
		t.Fatalf("WriteJSON() error = %v", err)
	}
	if strings.Contains(buf.String(), `\u003e`) || !strings.Contains(buf.String(), "->") {
		t.Errorf("arrow is escaped:\n%s", buf.String())
	}
}

func TestWriteJSON(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteJSON(&buf, "0.1.0", sampleReport()); err != nil {
		t.Fatalf("WriteJSON() error = %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, buf.String())
	}
	if got["schemaVersion"] != SchemaVersion || got["project"] != "my-backend" {
		t.Errorf("header fields = %v", got)
	}
	summary := got["summary"].(map[string]any)
	if summary["drift"] != float64(1) || summary["passed"] != float64(1) {
		t.Errorf("summary = %v", summary)
	}
	services := got["services"].([]any)
	api := services[1].(map[string]any)
	finding := api["findings"].([]any)[0].(map[string]any)
	if finding["kind"] != "port_mismatch" || finding["severity"] != "drift" {
		t.Errorf("finding = %v", finding)
	}
	if _, present := finding["container"]; present {
		t.Error("empty container name should be omitted")
	}
}

func TestWriteJSONUsesEmptyListsNotNull(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteJSON(&buf, "dev", drift.Report{Project: "p"}); err != nil {
		t.Fatalf("WriteJSON() error = %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "null") {
		t.Errorf("output contains null:\n%s", out)
	}
	if !strings.Contains(out, `"services": []`) || !strings.Contains(out, `"warnings": []`) {
		t.Errorf("empty lists missing:\n%s", out)
	}
}

func TestDisplayVersion(t *testing.T) {
	for in, want := range map[string]string{"0.1.0": "v0.1.0", "dev": "dev", "v1.2.3": "v1.2.3", "": ""} {
		if got := DisplayVersion(in); got != want {
			t.Errorf("DisplayVersion(%q) = %q, want %q", in, got, want)
		}
	}
}
