package security

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

// gitleaksDoc is the subset of the emitted config the tests decode, so every
// assertion reads the regex back out of the file gitleaks will see rather than
// the Go constant it was rendered from.
type gitleaksDoc struct {
	Extend struct {
		UseDefault bool `toml:"useDefault"`
	} `toml:"extend"`
	Allowlists []struct {
		Description string   `toml:"description"`
		RegexTarget string   `toml:"regexTarget"`
		Regexes     []string `toml:"regexes"`
	} `toml:"allowlists"`
}

func writeConfig(t *testing.T) (string, gitleaksDoc) {
	t.Helper()
	dir := t.TempDir()
	path, err := WriteGitleaksConfig(dir, []string{"testdata", "fixtures"})
	if err != nil {
		t.Fatal(err)
	}
	var doc gitleaksDoc
	if _, err := toml.DecodeFile(path, &doc); err != nil {
		t.Fatalf("emitted config does not parse as TOML: %v", err)
	}
	return path, doc
}

// TestGitleaksConfigExtendsDefault: [extend] useDefault = true is the only thing
// between --config and a zero-rule scan.
func TestGitleaksConfigExtendsDefault(t *testing.T) {
	_, doc := writeConfig(t)
	if !doc.Extend.UseDefault {
		t.Fatal("emitted gitleaks.toml must carry [extend] useDefault = true — without it --config disables every default rule")
	}
	if len(doc.Allowlists) != 1 {
		t.Fatalf("want exactly one [[allowlists]] table, got %d", len(doc.Allowlists))
	}
	if doc.Allowlists[0].RegexTarget != "line" {
		t.Fatalf("regexTarget = %q, want line", doc.Allowlists[0].RegexTarget)
	}
	if !strings.Contains(doc.Allowlists[0].Description, "testdata") || !strings.Contains(doc.Allowlists[0].Description, "fixtures") {
		t.Fatalf("description %q must name the skipped directories", doc.Allowlists[0].Description)
	}
}

// TestIdentityIDAllowlistShape pins the allowlist to exactly the identity-id
// shape. The regex under test is parsed back out of the written TOML: widening
// the class or length fails a NOT row, narrowing fails a match row.
func TestIdentityIDAllowlistShape(t *testing.T) {
	_, doc := writeConfig(t)
	if len(doc.Allowlists) != 1 || len(doc.Allowlists[0].Regexes) != 1 {
		t.Fatalf("want one allowlist with one regex, got %+v", doc.Allowlists)
	}
	re, err := regexp.Compile(doc.Allowlists[0].Regexes[0])
	if err != nil {
		t.Fatalf("emitted regex does not compile: %v", err)
	}
	if re.String() != IdentityIDAllowlist.String() {
		t.Fatalf("emitted regex %q differs from IdentityIDAllowlist %q", re.String(), IdentityIDAllowlist.String())
	}
	cases := []struct {
		line string
		want bool
	}{
		{`"key": "b91bfa24fdf586c0"`, true},
		{`"Key": "50919a010c495368"`, true},
		{`key = "919acc418a9a0821"`, true},
		{`id = 30dcd7db2eecf398`, true},
		{`"password": "08ec1d7666c48b32"`, false},
		{`token = "08ec1d7666c48b32"`, false},
		{`08ec1d7666c48b32`, false},
		{`"key": "08ec1d7666c48b3"`, false},                  // 15-hex
		{`"key": "08ec1d7666c48b32a"`, false},                // 17-hex
		{`"key": "08ec1d7666c48b32ab"`, false},               // 18-hex
		{`"key": "08ec1d7666c48b3208ec1d7666c48b32"`, false}, // 32-hex
		{`key = "AKIAIOSFODNN7EXAMPLE"`, false},
	}
	for _, c := range cases {
		if got := re.MatchString(c.line); got != c.want {
			t.Errorf("match(%q) = %v, want %v", c.line, got, c.want)
		}
	}
}

// TestGitleaksConfigNotPathExcluded pins the allowlist_scope decision: the
// emitted TOML has no `paths` key at any level, so .dross/, testdata/ and
// vendor/ stay inside the secret scanner's view.
func TestGitleaksConfigNotPathExcluded(t *testing.T) {
	path, _ := writeConfig(t)
	var generic map[string]any
	if _, err := toml.DecodeFile(path, &generic); err != nil {
		t.Fatal(err)
	}
	var walk func(prefix string, v any)
	walk = func(prefix string, v any) {
		switch x := v.(type) {
		case map[string]any:
			for k, child := range x {
				if k == "paths" {
					t.Errorf("emitted gitleaks.toml carries a `paths` key at %s — the allowlist must cover a shape, never a location", prefix+k)
				}
				walk(prefix+k+".", child)
			}
		case []map[string]any:
			for _, m := range x {
				walk(prefix, m)
			}
		case []any:
			for _, e := range x {
				walk(prefix, e)
			}
		}
	}
	walk("", generic)
}

// TestGitleaksConfigContained: the file lands at filepath.Join(runDir,
// GitleaksConfigName) through the pathfence seam, and a second call rewrites
// identical bytes. Containment here is lexical and applies to the name inside
// the run dir (the run dir itself is the containment root, as for report.md);
// pathfence deliberately accepts symlinks (symlink_resolution lock), so no
// symlink case is asserted.
func TestGitleaksConfigContained(t *testing.T) {
	dir := t.TempDir()
	first, err := WriteGitleaksConfig(dir, []string{"testdata"})
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, GitleaksConfigName); first != want {
		t.Fatalf("path = %q, want %q", first, want)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != GitleaksConfigName {
		t.Fatalf("run dir holds %v, want only %s", entries, GitleaksConfigName)
	}
	a, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	second, err := WriteGitleaksConfig(dir, []string{"testdata"})
	if err != nil {
		t.Fatalf("second write: %v", err)
	}
	b, err := os.ReadFile(second)
	if err != nil {
		t.Fatal(err)
	}
	if first != second || !reflect.DeepEqual(a, b) {
		t.Fatal("second call must return the same path with identical bytes")
	}
	if _, err := WriteGitleaksConfig("", nil); err == nil {
		t.Fatal("an empty run dir must be refused — it would write into the working directory")
	}
}

type gitleaksFinding struct {
	RuleID string `json:"RuleID"`
	File   string `json:"File"`
	Secret string `json:"Secret"`
}

func gitleaksOrSkip(t *testing.T) string {
	t.Helper()
	bin, err := exec.LookPath("gitleaks")
	if err != nil {
		t.Skip("gitleaks not on PATH — live allowlist check skipped (install gitleaks to run it)")
	}
	return bin
}

func runGitleaks(t *testing.T, bin string, args ...string) (int, []gitleaksFinding) {
	t.Helper()
	report := filepath.Join(t.TempDir(), "report.json")
	args = append(args, "--no-banner", "--exit-code", "1", "--report-format", "json", "--report-path", report)
	cmd := exec.Command(bin, args...)
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			t.Fatalf("gitleaks %v: %v\n%s", args, err, out)
		}
		code = ee.ExitCode()
	}
	if code != 0 && code != 1 {
		t.Fatalf("gitleaks %v exited %d:\n%s", args, code, out)
	}
	var findings []gitleaksFinding
	if b, err := os.ReadFile(report); err == nil && len(strings.TrimSpace(string(b))) > 0 {
		if err := json.Unmarshal(b, &findings); err != nil {
			t.Fatalf("report.json: %v\n%s", err, b)
		}
	}
	return code, findings
}

// TestGitleaksLiveAllowlist drives the real tool: with the emitted config the
// identity id in tests.json is silent while a planted stripe key still fires;
// without the config the identity id IS reported (positive control), so a
// config that disabled the rules (exit 0) or a regexTarget/regex the tool
// reads differently (an identity-id finding) both fail.
func TestGitleaksLiveAllowlist(t *testing.T) {
	bin := gitleaksOrSkip(t)
	tree := t.TempDir()
	if err := os.WriteFile(filepath.Join(tree, "tests.json"), []byte(`{"tests": [{"key": "b91bfa24fdf586c0", "name": "x"}]}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Concat-built so the literal never sits in the source tree; NOT the AWS
	// AKIA…EXAMPLE key, which gitleaks' default aws rule allowlists.
	stripe := strings.Join([]string{"sk", "test", "51H7abcDEF1234567890ghijklmnopQR"}, "_")
	if err := os.WriteFile(filepath.Join(tree, "leak.txt"), []byte("STRIPE="+stripe+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := WriteGitleaksConfig(t.TempDir(), []string{"testdata", "fixtures"})
	if err != nil {
		t.Fatal(err)
	}

	code, findings := runGitleaks(t, bin, "dir", tree, "--config", cfg)
	if code != 1 {
		t.Fatalf("with --config: exit %d, want 1 (the stripe key must still fire — exit 0 means the config disabled the default rules)", code)
	}
	var sawStripe bool
	for _, f := range findings {
		if strings.HasSuffix(f.File, "tests.json") {
			t.Errorf("identity id reported despite the allowlist: rule=%s secret=%s", f.RuleID, f.Secret)
		}
		if strings.HasSuffix(f.File, "leak.txt") {
			sawStripe = true
		}
	}
	if !sawStripe {
		t.Fatalf("stripe key not reported with --config; findings=%+v", findings)
	}

	code, findings = runGitleaks(t, bin, "dir", tree)
	if code != 1 {
		t.Fatalf("positive control: exit %d, want 1", code)
	}
	var sawIdentity bool
	for _, f := range findings {
		if strings.HasSuffix(f.File, "tests.json") {
			sawIdentity = true
		}
	}
	if !sawIdentity {
		t.Fatalf("positive control: gitleaks without --config did not report tests.json, so the allowlist test proves nothing; findings=%+v", findings)
	}
}

// TestGitleaksDrossTreeNoIdentityHits is c-4's zero-identity-shape-hits leg on
// dross itself: a git-history scan under the emitted config yields no finding
// whose Secret is a bare 16-hex id. Residual hits of other shapes are the secure
// run's to dismiss, not this test's to fail on.
func TestGitleaksDrossTreeNoIdentityHits(t *testing.T) {
	if testing.Short() {
		t.Skip("scans full git history; skipped under -short")
	}
	bin := gitleaksOrSkip(t)
	cfg, err := WriteGitleaksConfig(t.TempDir(), []string{"testdata", "fixtures"})
	if err != nil {
		t.Fatal(err)
	}
	_, findings := runGitleaks(t, bin, "git", repoRoot(t), "--config", cfg)
	identity := regexp.MustCompile(`^[0-9a-f]{16}$`)
	for _, f := range findings {
		if identity.MatchString(f.Secret) {
			t.Errorf("identity-shape hit survived the allowlist: rule=%s file=%s secret=%s", f.RuleID, f.File, f.Secret)
		}
	}
}
