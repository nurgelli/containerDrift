package drift

import (
	"fmt"
	"slices"
	"strings"

	"github.com/nurgelli/containerdrift/internal/model"
)

// compareMounts checks that every desired mount exists in the container with
// the same type, source and access mode. Extra mounts in the container are
// ignored: images declare anonymous volumes (VOLUME in a Dockerfile) that
// Compose never lists, and reporting them would be a false alarm.
func compareMounts(spec model.ServiceSpec, c model.ContainerState) []Finding {
	var expected, actual []string

	for _, want := range spec.Mounts {
		got, ok := findMount(c.Mounts, want.Target)
		if ok && mountsEqual(want, got) {
			continue
		}
		expected = append(expected, formatMount(want))
		if ok {
			actual = append(actual, formatMount(got))
		} else {
			actual = append(actual, fmt.Sprintf("(no mount at %s)", want.Target))
		}
	}

	if len(expected) == 0 {
		return nil
	}
	return []Finding{{
		Kind:      KindVolumes,
		Severity:  SeverityDrift,
		Container: c.Name,
		Message:   labelled(c, "Volume mount mismatch"),
		Expected:  expected,
		Actual:    actual,
	}}
}

func findMount(mounts []model.Mount, target string) (model.Mount, bool) {
	i := slices.IndexFunc(mounts, func(m model.Mount) bool { return m.Target == target })
	if i < 0 {
		return model.Mount{}, false
	}
	return mounts[i], true
}

func mountsEqual(want, got model.Mount) bool {
	if want.Type != got.Type || want.ReadOnly != got.ReadOnly {
		return false
	}
	switch want.Type {
	case "bind":
		return sameHostPath(want.Source, got.Source)
	default:
		// An anonymous volume has no source to compare.
		return want.Source == "" || want.Source == got.Source
	}
}

// sameHostPath compares bind-mount sources. Path separators and a trailing
// slash are ignored; on Windows-style paths (drive letter) case is ignored too.
func sameHostPath(a, b string) bool {
	na, nb := normalizePath(a), normalizePath(b)
	if isWindowsPath(na) && isWindowsPath(nb) {
		return strings.EqualFold(na, nb)
	}
	return na == nb
}

func normalizePath(p string) string {
	p = strings.ReplaceAll(p, `\`, "/")
	if len(p) > 1 {
		p = strings.TrimRight(p, "/")
	}
	return p
}

func isWindowsPath(p string) bool {
	return len(p) >= 2 && p[1] == ':' && (p[0] >= 'a' && p[0] <= 'z' || p[0] >= 'A' && p[0] <= 'Z')
}

func formatMount(m model.Mount) string {
	source := m.Source
	if source == "" {
		source = "(anonymous)"
	}
	mode := "rw"
	if m.ReadOnly {
		mode = "ro"
	}
	return fmt.Sprintf("%s %s -> %s (%s)", m.Type, source, m.Target, mode)
}
