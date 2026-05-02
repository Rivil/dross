package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestVersionStringFormat(t *testing.T) {
	// Stable format — doctor and scripts may grep this.
	got := VersionString()
	for _, want := range []string{"dross ", "(commit ", "built "} {
		if !strings.Contains(got, want) {
			t.Errorf("VersionString missing %q: %s", want, got)
		}
	}
}

func TestVersionCmdPrintsToStdout(t *testing.T) {
	c := VersionCmd()
	var buf bytes.Buffer
	c.SetOut(&buf)
	if err := c.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(buf.String(), VersionString()) {
		t.Errorf("output %q missing version string %q", buf.String(), VersionString())
	}
}

func TestVersionVarsOverridable(t *testing.T) {
	// Ensures the ldflags target vars are package-level and writable —
	// guards against accidental refactor that would silently break -ldflags.
	prevV, prevC, prevD := Version, Commit, Date
	t.Cleanup(func() { Version, Commit, Date = prevV, prevC, prevD })

	Version = "9.9.9.9"
	Commit = "deadbeef"
	Date = "2099-01-01T00:00:00Z"
	got := VersionString()
	if !strings.Contains(got, "9.9.9.9") || !strings.Contains(got, "deadbeef") {
		t.Errorf("override not reflected: %s", got)
	}
}
