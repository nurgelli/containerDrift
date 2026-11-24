// Package report renders a drift report as human-readable text or JSON.
package report

import (
	"fmt"
	"io"
	"strings"

	"github.com/nurgelli/containerdrift/internal/drift"
)

// DisplayVersion formats the build version for the report header:
// "0.1.0" becomes "v0.1.0", while "dev" stays "dev".
func DisplayVersion(version string) string {
	if version != "" && version[0] >= '0' && version[0] <= '9' {
		return "v" + version
	}
	return version
}

// WriteTable writes the human-readable report.
func WriteTable(w io.Writer, version string, r drift.Report) error {
	var b strings.Builder

	fmt.Fprintf(&b, "ContainerDrift %s\n\nProject: %s\n", DisplayVersion(version), r.Project)

	for _, svc := range r.Services {
		fmt.Fprintf(&b, "\n[%s] %s\n", tag(svc.Status), svc.Name)

		// Only passing services list what matched; for the others the
		// findings are what matters.
		if svc.Status == drift.StatusPass {
			for _, m := range svc.Matches {
				fmt.Fprintf(&b, "  %s\n", m)
			}
		}
		for _, f := range svc.Findings {
			fmt.Fprintf(&b, "  %s\n", f.Message)
			writeList(&b, "Expected", f.Expected)
			writeList(&b, "Actual", f.Actual)
		}
	}

	if len(r.Warnings) > 0 {
		b.WriteString("\nWarnings:\n")
		for _, w := range r.Warnings {
			fmt.Fprintf(&b, "  - %s\n", w)
		}
	}

	s := r.Summary()
	fmt.Fprintf(&b, "\nSummary:\n  Services: %d\n  Passed: %d\n  Drift detected: %d\n  Warnings: %d\n",
		s.Services, s.Passed, s.Drift, s.Warnings)
	b.WriteString("\nScan completed.\n")

	_, err := io.WriteString(w, b.String())
	return err
}

func tag(s drift.Status) string {
	switch s {
	case drift.StatusPass:
		return "PASS"
	case drift.StatusDrift:
		return "DRIFT"
	default:
		return "WARN"
	}
}

func writeList(b *strings.Builder, title string, lines []string) {
	if len(lines) == 0 {
		return
	}
	fmt.Fprintf(b, "\n  %s:\n", title)
	for _, l := range lines {
		fmt.Fprintf(b, "    %s\n", l)
	}
}
