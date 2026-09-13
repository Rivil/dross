package security

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/Rivil/dross/internal/pathfence"
)

// GitleaksConfigName is the file `dross security run` writes into the run
// directory and the secure prompt hands to gitleaks via --config. It lives in
// the run dir, never at the repo root: every adopter's .dross/tests.json carries
// the same 16-hex identity ids, so a committed .gitleaks.toml would fix only the
// repo it was committed to (allowlist_delivery decision).
const GitleaksConfigName = "gitleaks.toml"

// identityIDPattern is the one shape the emitted allowlist silences: a 16-hex
// value sitting in an id/key context (`"key": "b91bfa24fdf586c0"`, `id =
// 30dcd7db2eecf398`). The trailing \b is load-bearing — without it a 17-, 18- or
// 32-hex value satisfies {16} and is silenced too, verified live on gitleaks
// 8.30.1. The class stays [0-9a-f] and the context stays id|key: a password or
// token line with the same value is still a finding for the secure run to judge.
const identityIDPattern = `(?i)\b(id|key)\b["']?\s*[:=]\s*["']?[0-9a-f]{16}\b["']?`

// IdentityIDAllowlist is identityIDPattern compiled, so callers can test a line
// against exactly what the emitted config carries.
var IdentityIDAllowlist = regexp.MustCompile(identityIDPattern)

// renderGitleaksConfig renders the per-run gitleaks config: it extends the
// default rule set (drop [extend] and --config yields a zero-rule scan) and adds
// one line-targeted allowlist for the identity-id shape. skipped names the
// directories dross's own scanners scope out and appears only in the
// description — never as a `paths` key: the allowlist covers a shape, not a
// location, so .dross/ stays scanned (allowlist_scope decision).
func renderGitleaksConfig(skipped []string) string {
	var b strings.Builder
	b.WriteString("# Written by `dross security run`; passed to gitleaks via --config.\n")
	b.WriteString("# Extends the default rules and allowlists one shape only.\n\n")
	b.WriteString("[extend]\nuseDefault = true\n\n")
	b.WriteString("[[allowlists]]\n")
	fmt.Fprintf(&b, "description = %q\n",
		fmt.Sprintf("dross identity ids (16-hex in id/key context); dross scanners skip directories: %s; no path is excluded here",
			strings.Join(skipped, ", ")))
	b.WriteString("regexTarget = \"line\"\n")
	fmt.Fprintf(&b, "regexes = ['''%s''']\n", identityIDPattern)
	return b.String()
}

// WriteGitleaksConfig writes the per-run gitleaks config into runDir through
// pathfence and returns the written path. It is idempotent: a second call
// rewrites identical bytes.
func WriteGitleaksConfig(runDir string, skipped []string) (string, error) {
	if strings.TrimSpace(runDir) == "" {
		// An empty root would join to a bare relative name and land in the
		// process working directory — a config outside any run dir.
		return "", fmt.Errorf("write gitleaks config: empty run directory")
	}
	c, err := pathfence.Contain(runDir, "run directory", GitleaksConfigName)
	if err != nil {
		return "", err
	}
	if err := pathfence.WriteFile(c, []byte(renderGitleaksConfig(skipped)), 0o644); err != nil {
		return "", fmt.Errorf("write gitleaks config: %w", err)
	}
	return c.String(), nil
}
