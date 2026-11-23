package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestRootCmdVersion(t *testing.T) {
	var out bytes.Buffer
	cmd := NewRootCmd("1.2.3-test")
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--version"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(out.String(), "1.2.3-test") {
		t.Errorf("output %q does not contain the version", out.String())
	}
}
