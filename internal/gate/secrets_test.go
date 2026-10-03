package gate

import (
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/gitrun"
)

// gatesEnv runs only the named registered gates, so each table judges one
// rule — through its real registration, not a copy.
func gatesEnv(t *testing.T, names ...string) Env {
	t.Helper()
	var gs []Gate
	for _, n := range names {
		g, ok := Lookup(n)
		if !ok {
			t.Fatalf("gate %q is not registered", n)
		}
		gs = append(gs, g)
	}
	return Env{Home: t.TempDir(), Gates: gs}
}

func TestSecretStreamRefuses(t *testing.T) {
	e := gatesEnv(t, "secret-stream")
	for _, line := range []string{
		"pass-cli item view x",
		"pass-cli item view x | head",
		"pass-cli item view x >f",
		"pass-cli item view x 2>/dev/null",
		"pass-cli item view x 2>&1 >f",
		"pass-cli item view x 2>&1 | head",
		"pass-cli item view x |& head",
		"FOO=1 pass-cli item view x",
		"cd d && pass-cli item view x >f",
		"/opt/bin/pass-cli item view x | cat",
		"pass-cli item list | head",
		"KEY=$(pass-cli item view x 2>/dev/null)",
	} {
		res := Check(bash(t, line, t.TempDir()), e)
		text := res.Text()
		if res.Allowed() || !strings.Contains(text, "secret-stream") || !strings.Contains(text, ">file 2>/dev/null") {
			t.Errorf("%q: %q, want a secret-stream refusal naming `>file 2>/dev/null`", line, text)
		}
	}
}

func TestSecretStreamAllows(t *testing.T) {
	e := gatesEnv(t, "secret-stream")
	for _, line := range []string{
		"pass-cli item view x >f 2>/dev/null",
		"pass-cli item view x >f 2>err.log",
		"pass-cli item view x >f 2>&1",
		"pass-cli item view x &>f",
		"make build 2>&1",
		"go test ./... 2>&1 | tail",
		"echo hi; pass-cli item view x >f 2>/dev/null",
		`echo "pass-cli x"`,
	} {
		if res := Check(bash(t, line, t.TempDir()), e); !res.Allowed() || len(res.Warnings) != 0 {
			t.Errorf("%q: %q %q, want silence", line, res.Text(), res.Warnings)
		}
	}
}

func TestSecretPattern(t *testing.T) {
	pats := Defaults().SecretPaths
	for _, p := range []string{
		".env", "prod.env", ".env.local", ".envrc", "server.pem", "tls.key",
		"/home/u/.ssh/id_ed25519", "/home/u/.ssh/id_ecdsa_sk", "/home/u/.config/sops/age/keys.txt", "ops.agekey",
	} {
		if SecretPattern(p, pats) == "" {
			t.Errorf("%s matched no secret pattern", p)
		}
	}
	for _, p := range []string{
		"/home/u/.ssh/id_ed25519.pub", "internal/auth/id_token.go", "id_generator.ts",
		".env.example", ".env.sample", ".env.template", ".env.dist", "deploy/prod.env.example",
		"envoy.yaml", "main.go", "keys.txt", "notes/age/keys.txt",
	} {
		if pat := SecretPattern(p, pats); pat != "" {
			t.Errorf("%s matched %q, want no match", p, pat)
		}
	}
}

func TestSecretReadRead(t *testing.T) {
	e := gatesEnv(t, "secret-read")
	read := func(p string) Result {
		return Check(payload(t, "Read", map[string]any{"file_path": p}, t.TempDir()), e)
	}
	for _, p := range []string{"/w/.env", "/w/prod.env", "/w/.env.local", "/w/.envrc", "/w/server.pem", "/w/tls.key",
		"/home/u/.ssh/id_ed25519", "/home/u/.ssh/id_ecdsa_sk", "/home/u/.config/sops/age/keys.txt", "/w/ops.agekey"} {
		if res := read(p); res.Allowed() || !strings.Contains(res.Text(), "secret-read") {
			t.Errorf("Read %s: %q, want a secret-read refusal", p, res.Text())
		}
	}
	for _, p := range []string{"/home/u/.ssh/id_ed25519.pub", "/w/internal/auth/id_token.go", "/w/id_generator.ts",
		"/w/.env.example", "/w/.env.sample", "/w/.env.template", "/w/.env.dist", "/w/envoy.yaml", "/w/main.go", "/w/keys.txt"} {
		if res := read(p); !res.Allowed() {
			t.Errorf("Read %s refused: %q", p, res.Text())
		}
	}
}

func TestSecretReadBash(t *testing.T) {
	e := gatesEnv(t, "secret-read")
	for _, line := range []string{
		"cat .env", "head -n 3 config/prod.env", "tail -f .env.local", "less id_rsa",
		"sed -n 1p ~/.ssh/id_ed25519", "cat < .env", "cd x && cat .env", "X=$(cat .env)",
	} {
		res := Check(bash(t, line, t.TempDir()), e)
		if res.Allowed() || !strings.Contains(res.Text(), "secret-read") || !strings.Contains(res.Text(), "wc -c") {
			t.Errorf("%q: %q, want a secret-read refusal pointing at a structural check", line, res.Text())
		}
	}
	for _, line := range []string{
		"source .env && make", ". ./.env", "wc -c .env", "shasum -a 256 .env", "grep -q KEY .env",
		"cat ~/.ssh/id_ed25519.pub", "cat .env.example", "echo cat .env",
	} {
		if res := Check(bash(t, line, t.TempDir()), e); !res.Allowed() {
			t.Errorf("%q refused: %q", line, res.Text())
		}
	}
}

// TestSecretPartialPosture: a line the scanner could not read is refused when
// it holds a claimed token — naming the parse problem — and left alone when it
// does not.
func TestSecretPartialPosture(t *testing.T) {
	e := gatesEnv(t, "secret-stream", "secret-read")
	for _, line := range []string{`pass-cli item view x 2>&1 "oops`, `cat .env "oops`, `echo "oops; pass-cli x`} {
		res := Check(bash(t, line, t.TempDir()), e)
		if res.Allowed() || !strings.Contains(res.Text(), "unterminated double quote") {
			t.Errorf("%q: %q, want a refusal naming the parse problem", line, res.Text())
		}
	}
	for _, line := range []string{`echo "oops`, `git commit -m "half`} {
		if res := Check(bash(t, line, t.TempDir()), e); !res.Allowed() {
			t.Errorf("%q (no claimed token) refused: %q", line, res.Text())
		}
	}
}

// TestSecretGuardsFireEverywhere: guard_scope — no .dross ancestor needed, and
// neither guard spawns git to decide.
func TestSecretGuardsFireEverywhere(t *testing.T) {
	var argv [][]string
	prev := gitrun.ArgvRecorder
	gitrun.ArgvRecorder = func(a []string) { argv = append(argv, a) }
	t.Cleanup(func() { gitrun.ArgvRecorder = prev })

	e := gatesEnv(t, "secret-stream", "secret-read")
	nowhere := t.TempDir()
	if res := Check(bash(t, "pass-cli item view x | head", nowhere), e); res.Allowed() {
		t.Error("secret-stream went quiet outside a dross repo")
	}
	if res := Check(payload(t, "Read", map[string]any{"file_path": nowhere + "/.env"}, nowhere), e); res.Allowed() {
		t.Error("secret-read went quiet outside a dross repo")
	}
	if len(argv) != 0 {
		t.Errorf("the secret guards spawned git: %q", argv)
	}
}

func TestSecretToolsExtension(t *testing.T) {
	e := gatesEnv(t, "secret-stream")
	if res := Check(bash(t, "op read x | head", t.TempDir()), e); !res.Allowed() {
		t.Fatalf("op is not a default secret tool, yet: %q", res.Text())
	}
	writeLists(t, e.Home, "secret_tools = [\"op\"]\n")
	if res := Check(bash(t, "op read x | head", t.TempDir()), e); res.Allowed() {
		t.Error("adding op to secret_tools in gates.toml did not guard it")
	}
}
