package cmd

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// synthGitlabToken builds a gitlab-pat-shaped value at runtime. It is
// assembled, never spelled: a literal token shape in a test file is exactly
// what the phase's self-scan (t-8) would trip on.
func synthGitlabToken() string {
	return "glpat-" + strings.Repeat("Ab1", 7)
}

// assertNoTokenEcho fails if any 6-byte window of the token's variable part
// (everything after the fixed prefix) appears in out. The prefix itself is
// fair game — the fingerprint names it on purpose.
func assertNoTokenEcho(t *testing.T, out, token, prefix string) {
	t.Helper()
	variable := strings.TrimPrefix(token, prefix)
	for i := 0; i+6 <= len(variable); i++ {
		if strings.Contains(out, variable[i:i+6]) {
			t.Fatalf("output echoes the token (window %d): %s", i, out)
		}
	}
}

// initProject brings up a clean, validate-green project in dir and chdirs
// into it. Callers layer the artifact under test on top.
func initProject(t *testing.T, dir string) {
	t.Helper()
	chdir(t, dir)
	if err := runCmd(t, Init()); err != nil {
		t.Fatal(err)
	}
	mustRunSet(t, "project.name", "test-app")
	mustRunSet(t, "runtime.mode", "native")
}

// runValidate runs Validate and returns its error plus everything it printed.
func runValidate(t *testing.T) (string, error) {
	t.Helper()
	var err error
	out := captureStdout(t, func() { err = runCmd(t, Validate()) })
	return out, err
}

// TestValidateRefusesOnArtifactSecret pins the non-git fallback walk and the
// location format: a token in a file validate hand-lists nowhere — a notes.md
// under a phase dir — is found by the walk and reported as
// `<rule> at .dross/<path>:<line> (len=…`, with nothing of the value echoed.
func TestValidateRefusesOnArtifactSecret(t *testing.T) {
	dir := t.TempDir()
	initProject(t, dir) // no git: exercises listDrossArtifactsWalk

	tok := synthGitlabToken()
	mustWrite(t, filepath.Join(dir, ".dross", "phases", "p", "notes.md"),
		"# notes\n\nsome prose\ntoken: "+tok+"\nmore prose\n")

	out, err := runValidate(t)
	if err == nil {
		t.Fatalf("validate passed over an artifact carrying a token; output:\n%s", out)
	}
	want := "gitlab-pat at .dross/phases/p/notes.md:4 (len="
	if !strings.Contains(out, want) {
		t.Fatalf("output lacks %q:\n%s", want, out)
	}
	if !strings.Contains(out, "secret: ") {
		t.Fatalf("hit is not reported under the secret: prefix:\n%s", out)
	}
	assertNoTokenEcho(t, out, tok, "glpat-")
}

// TestScopeIsTrackedPlusStageable pins the git enumeration flags: tracked
// files and stageable untracked files are in scope; gitignored files are not.
// Dropping --others loses the untracked note; dropping --exclude-standard
// pulls in the ignored run report. Either way the hit count changes.
func TestScopeIsTrackedPlusStageable(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir, "git@github.com:Rivil/x.git")
	initProject(t, dir)
	if err := ensureDrossGitignore(dir); err != nil {
		t.Fatal(err)
	}
	// The dross repo's own .gitignore keeps security run reports out of
	// history; mirror that so the ignored branch is exercised.
	f, err := os.OpenFile(filepath.Join(dir, ".gitignore"), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("\n.dross/security/\n"); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()

	tok := synthGitlabToken()
	// Tracked and clean.
	mustWrite(t, filepath.Join(dir, ".dross", "board.json"), `{"issues": []}`+"\n")
	mustGit(t, dir, "add", ".dross/board.json", ".gitignore")
	mustGit(t, dir, "commit", "-q", "-m", "seed")
	// Untracked, stageable, carries a token.
	mustWrite(t, filepath.Join(dir, ".dross", "notes.md"), "t = "+tok+"\n")
	// Ignored, carries a token.
	mustWrite(t, filepath.Join(dir, ".dross", "security", "run1", "gitleaks.json"),
		`{"Secret": "`+tok+`"}`+"\n")

	hits, err := scanDrossArtifacts(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("want exactly 1 hit (the untracked note), got %d: %v", len(hits), hits)
	}
	if hits[0].Location != ".dross/notes.md" {
		t.Fatalf("hit location = %q, want .dross/notes.md", hits[0].Location)
	}
}

// TestValidateAllowMarkerHonouredInToml pins that the line marker works
// inside a toml artifact — the shape a user reaches for when a spec has to
// quote a credential-looking string on purpose.
func TestValidateAllowMarkerHonouredInToml(t *testing.T) {
	dir := t.TempDir()
	initProject(t, dir)

	tok := synthGitlabToken()
	spec := filepath.Join(dir, ".dross", "phases", "p", "spec.toml")
	mustWrite(t, spec, "[phase]\nid = \"p\"\ntitle = \"p\"\ntext = \""+tok+"\" # dross:allow-secret\n")

	if out, err := runValidate(t); err != nil {
		t.Fatalf("marked line still fails validate: %v\n%s", err, out)
	} else if strings.Contains(out, "secret:") {
		t.Fatalf("marked line still reported:\n%s", out)
	}

	// Same file, marker removed: the carve-out is doing work.
	mustWrite(t, spec, "[phase]\nid = \"p\"\ntitle = \"p\"\ntext = \""+tok+"\"\n")
	if out, err := runValidate(t); err == nil {
		t.Fatalf("unmarked line passed validate:\n%s", out)
	} else if !strings.Contains(out, "secret: gitlab-pat at .dross/phases/p/spec.toml:4") {
		t.Fatalf("unmarked line not reported at its location:\n%s", out)
	}
}

// TestScanErrorIsARefusalNotASkip pins that an artifact the scanner cannot
// read fails validate rather than being silently passed over.
func TestScanErrorIsARefusalNotASkip(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("mode 000 is not a read barrier on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root reads mode-000 files")
	}
	dir := t.TempDir()
	initProject(t, dir)

	locked := filepath.Join(dir, ".dross", "x.toml")
	mustWrite(t, locked, "a = 1\n")
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o644) })

	out, err := runValidate(t)
	if err == nil {
		t.Fatalf("validate passed over an unreadable artifact:\n%s", out)
	}
	if !strings.Contains(out, "secret scan:") || !strings.Contains(out, "permission denied") {
		t.Fatalf("unreadable artifact not reported as a secret-scan refusal:\n%s", out)
	}
}

// TestValidateSecretProblemsAreLast pins that the scan is not short-circuited
// by an earlier structural problem: a malformed spec in one phase and a token
// in another both print.
func TestValidateSecretProblemsAreLast(t *testing.T) {
	dir := t.TempDir()
	initProject(t, dir)

	mustWrite(t, filepath.Join(dir, ".dross", "phases", "bad", "spec.toml"), "this is not ][ valid toml")
	tok := synthGitlabToken()
	mustWrite(t, filepath.Join(dir, ".dross", "phases", "leaky", "notes.md"), tok+"\n")

	out, err := runValidate(t)
	if err == nil {
		t.Fatalf("validate passed:\n%s", out)
	}
	badIdx := strings.Index(out, filepath.Join("phases", "bad", "spec.toml"))
	secretIdx := strings.Index(out, "secret: gitlab-pat at .dross/phases/leaky/notes.md:1")
	if badIdx < 0 || secretIdx < 0 {
		t.Fatalf("want both the malformed-spec problem and the secret problem:\n%s", out)
	}
	if secretIdx < badIdx {
		t.Fatalf("secret problems should print after structural ones:\n%s", out)
	}
	if !strings.Contains(err.Error(), "2 problem(s)") {
		t.Fatalf("want 2 problems counted, got %v", err)
	}
}
