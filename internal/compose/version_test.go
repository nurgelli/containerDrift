package compose

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestParseVersion(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    Version
		wantErr bool
	}{
		{"desktop suffix", "2.38.1-desktop.1\n", Version{2, 38, 1, "2.38.1-desktop.1"}, false},
		{"leading v", "v2.40.0", Version{2, 40, 0, "v2.40.0"}, false},
		{"plain", "5.0.1", Version{5, 0, 1, "5.0.1"}, false},
		{"empty", "", Version{}, true},
		{"too few parts", "2.38", Version{}, true},
		{"not numbers", "a.b.c", Version{}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseVersion(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseVersion(%q) error = %v, wantErr %v", tt.in, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("parseVersion(%q) = %+v, want %+v", tt.in, got, tt.want)
			}
		})
	}
}

func TestReaderVersion(t *testing.T) {
	notFound := fmt.Errorf("%w: docker executable not found in PATH", ErrUnavailable)
	noPlugin := &CommandError{Args: []string{"compose", "version", "--short"}, ExitCode: 1, Stderr: "'compose' is not a docker command"}

	tests := []struct {
		name       string
		out        string
		err        error
		wantMajor  int
		wantErrIs  error
		wantCmdErr bool
		wantAnyErr bool
	}{
		{name: "ok", out: "2.38.1-desktop.1\n", wantMajor: 2},
		{name: "docker missing", err: notFound, wantErrIs: ErrUnavailable, wantAnyErr: true},
		{name: "compose plugin missing", err: noPlugin, wantErrIs: ErrUnavailable, wantCmdErr: true, wantAnyErr: true},
		{name: "garbage output", out: "hello", wantAnyErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &Reader{run: func(context.Context, ...string) ([]byte, error) {
				return []byte(tt.out), tt.err
			}}

			got, err := r.Version(context.Background())
			if (err != nil) != tt.wantAnyErr {
				t.Fatalf("Version() error = %v, want error: %v", err, tt.wantAnyErr)
			}
			if tt.wantErrIs != nil && !errors.Is(err, tt.wantErrIs) {
				t.Errorf("error %v does not match %v", err, tt.wantErrIs)
			}
			if tt.wantCmdErr {
				var cmdErr *CommandError
				if !errors.As(err, &cmdErr) {
					t.Errorf("error %v does not contain a *CommandError", err)
				}
			}
			if err == nil && got.Major != tt.wantMajor {
				t.Errorf("Major = %d, want %d", got.Major, tt.wantMajor)
			}
		})
	}
}
