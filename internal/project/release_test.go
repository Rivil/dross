package project

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// releaseScript runs scripts/release-version.sh against a project.toml holding
// src, the way release.yml does: from the repo root, on nothing but a POSIX
// shell and the system directories. ok is false when the script refuses.
func releaseScript(t *testing.T, src string) (tag string, ok bool) {
	t.Helper()
	script, err := filepath.Abs(filepath.Join("..", "..", "scripts", "release-version.sh"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".dross"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".dross", File), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", script)
	cmd.Dir = dir
	cmd.Env = []string{"PATH=/usr/bin:/bin"}
	out, err := cmd.Output()
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(out)), true
}

// ReleaseTag must project exactly what release.yml's script does, so the
// chore guard's "would this cut a release?" is the release workflow's answer.
func TestReleaseTagMatchesReleaseScript(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"4-part", "[project]\n  name = \"dross\"\n  version = \"1.7.19.0\"\n", "v1.7.19"},
		{"3-part", "[project]\n  version = \"0.6.13\"\n", "v0.6.13"},
		{"version under [stack] first", "[stack]\n  version = \"9.9.9.9\"\n\n[project]\n  name = \"dross\"\n  version = \"1.2.3.4\"\n\n[remote]\n  version = \"8.8.8\"\n", "v1.2.3"},
		{"missing key", "[project]\n  name = \"dross\"\n\n[stack]\n  version = \"9.9.9.9\"\n", ""},
		{"trailing comment", "[project]\n  version = \"1.7.20.0\"  # phase main-branch-protection\n", "v1.7.20"},
		{"crlf", "[project]\r\n  version = \"2.0.1.3\"\r\n", "v2.0.1"},
		{"empty version", "[project]\n  version = \"\"\n", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scriptTag, scriptOK := releaseScript(t, tc.src)
			got, err := ReleaseTag([]byte(tc.src))
			if (err == nil) != scriptOK || got != scriptTag {
				t.Errorf("ReleaseTag = %q (err %v); release-version.sh = %q (ok %v)", got, err, scriptTag, scriptOK)
			}
			if got != tc.want {
				t.Errorf("ReleaseTag = %q, want %q", got, tc.want)
			}
		})
	}
}

// A quick task's .internal bump cuts no release; a minor bump does.
func TestReleaseTagBumpTable(t *testing.T) {
	tag := func(v string) string {
		t.Helper()
		got, err := ReleaseTag([]byte("[project]\n  version = \"" + v + "\"\n"))
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	for _, tc := range []struct {
		from, to string
		release  bool
	}{
		{"1.7.19.0", "1.7.19.1", false},
		{"1.7.19.1", "1.8.0.0", true},
		{"1.7.19.0", "1.7.20.0", true},
		{"1.7.19.3", "1.7.19.3", false},
	} {
		if cuts := tag(tc.from) != tag(tc.to); cuts != tc.release {
			t.Errorf("%s → %s: cuts a release = %v, want %v", tc.from, tc.to, cuts, tc.release)
		}
	}
}
