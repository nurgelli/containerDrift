package cli

import (
	"errors"
	"io"
	"testing"
)

func TestScanCmd(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr error
	}{
		{"default format reaches the stub", []string{"scan"}, errNotImplemented},
		{"json format is accepted", []string{"scan", "--format", "json"}, errNotImplemented},
		{"unknown format is rejected", []string{"scan", "--format", "xml"}, errInvalidFormat},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := NewRootCmd("test")
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			cmd.SetArgs(tt.args)

			err := cmd.Execute()
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("Execute() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}
