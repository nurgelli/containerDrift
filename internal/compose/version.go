package compose

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Version is a parsed Docker Compose CLI version.
type Version struct {
	Major, Minor, Patch int
	Raw                 string
}

func (v Version) String() string { return v.Raw }

// Reader obtains the Compose configuration by running the Docker Compose CLI.
type Reader struct {
	run runner
}

// NewReader returns a Reader that runs the real docker executable.
func NewReader() *Reader {
	return &Reader{run: execRunner}
}

// Version reports the installed Compose CLI version.
func (r *Reader) Version(ctx context.Context) (Version, error) {
	out, err := r.run(ctx, "compose", "version", "--short")
	if err != nil {
		var cmdErr *CommandError
		if errors.As(err, &cmdErr) {
			// docker exists but the compose subcommand failed (for example
			// when the Compose plugin is not installed).
			return Version{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
		}
		return Version{}, err
	}
	return parseVersion(string(out))
}

func parseVersion(s string) (Version, error) {
	raw := strings.TrimSpace(s)
	core := strings.TrimPrefix(raw, "v")
	// Drop suffixes such as "-desktop.1" or build metadata.
	if i := strings.IndexAny(core, "-+"); i >= 0 {
		core = core[:i]
	}

	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return Version{}, fmt.Errorf("unrecognized compose version %q", raw)
	}

	var nums [3]int
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return Version{}, fmt.Errorf("unrecognized compose version %q", raw)
		}
		nums[i] = n
	}
	return Version{Major: nums[0], Minor: nums[1], Patch: nums[2], Raw: raw}, nil
}
