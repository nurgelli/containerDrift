package compose

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// ErrUnavailable reports that the Docker Compose CLI cannot be used.
var ErrUnavailable = errors.New("docker compose is not available")

// CommandError describes a docker invocation that exited with a failure.
type CommandError struct {
	Args     []string
	ExitCode int
	Stderr   string
}

const maxStderrLines = 5

func (e *CommandError) Error() string {
	return fmt.Sprintf("docker %s failed (exit code %d): %s",
		strings.Join(e.Args, " "), e.ExitCode, firstLines(e.Stderr, maxStderrLines))
}

// firstLines keeps error messages short; the full text stays in the Stderr
// field for callers that need it.
func firstLines(s string, n int) string {
	lines := strings.Split(strings.ReplaceAll(strings.TrimSpace(s), "\r\n", "\n"), "\n")
	if len(lines) <= n {
		return strings.Join(lines, "\n")
	}
	return strings.Join(lines[:n], "\n") + "\n..."
}

// runner executes the docker CLI with the given arguments and returns stdout.
// It is a function type so tests can replace it without running Docker.
type runner func(ctx context.Context, args ...string) ([]byte, error)

func execRunner(ctx context.Context, args ...string) ([]byte, error) {
	// Arguments are passed separately; no shell is involved, so user input
	// such as file paths cannot be interpreted as shell syntax.
	cmd := exec.CommandContext(ctx, "docker", args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// A cancelled or timed-out context kills the process; report that
		// cause instead of the misleading "signal: killed" exit error.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("docker %s: %w", strings.Join(args, " "), ctxErr)
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil, &CommandError{Args: args, ExitCode: exitErr.ExitCode(), Stderr: stderr.String()}
		}
		if errors.Is(err, exec.ErrNotFound) {
			return nil, fmt.Errorf("%w: docker executable not found in PATH", ErrUnavailable)
		}
		return nil, fmt.Errorf("running docker %s: %w", strings.Join(args, " "), err)
	}
	return stdout.Bytes(), nil
}
