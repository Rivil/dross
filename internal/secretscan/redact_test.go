package secretscan_test

import (
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/secretscan"
)

// redactClean asserts the redacted text passes the scanner and is a fixed
// point of Redact.
func redactClean(t *testing.T, in string) (string, []secretscan.Hit) {
	t.Helper()
	out, hits := secretscan.Redact(in)
	if again := secretscan.ScanString("redacted", out); len(again) != 0 {
		t.Errorf("redacted text still scans dirty: %v\n%q", again, out)
	}
	if twice, _ := secretscan.Redact(out); twice != out {
		t.Errorf("Redact is not idempotent:\n once %q\ntwice %q", out, twice)
	}
	return out, hits
}

func TestRedactCoversEveryRule(t *testing.T) {
	corpus := hitCorpus()
	for _, r := range secretscan.Rules() {
		t.Run(r.Name, func(t *testing.T) {
			line, ok := corpus[r.Name]
			if !ok {
				t.Fatalf("rule %s has no redaction case", r.Name)
			}
			value := r.Regex.FindStringSubmatch(line)[1]
			out, hits := redactClean(t, line)
			if strings.Contains(out, value) {
				t.Errorf("the value survives: %q", out)
			}
			if !strings.Contains(out, "[redacted "+r.Name+"]") {
				t.Errorf("no [redacted %s] in %q", r.Name, out)
			}
			if len(hits) != 1 || hits[0].Rule != r.Name || hits[0].Line != 1 {
				t.Errorf("hits = %v, want one %s hit on line 1", hits, r.Name)
			}
		})
	}
}

func TestRedactAllMatchesOnALine(t *testing.T) {
	other := "ghp_" + strings.Repeat("Z9y8", 9)
	t.Run("two of one rule", func(t *testing.T) {
		out, hits := redactClean(t, "first "+ghTok()+" then "+other+" end")
		if out != "first [redacted github-token] then [redacted github-token] end" {
			t.Errorf("out = %q", out)
		}
		if len(hits) != 2 {
			t.Errorf("hits = %v, want 2", hits)
		}
	})
	t.Run("two rules", func(t *testing.T) {
		out, hits := redactClean(t, ghTok()+" "+slackTok())
		if out != "[redacted github-token] [redacted slack-token]" {
			t.Errorf("out = %q", out)
		}
		if len(hits) != 2 || hits[0].Rule != "github-token" || hits[1].Rule != "slack-token" {
			t.Errorf("hits = %v, want github-token then slack-token", hits)
		}
	})
	t.Run("every line", func(t *testing.T) {
		out, hits := redactClean(t, ghTok()+"\nclean\n"+other+"\n")
		if out != "[redacted github-token]\nclean\n[redacted github-token]\n" {
			t.Errorf("out = %q", out)
		}
		if len(hits) != 2 || hits[0].Line != 1 || hits[1].Line != 3 {
			t.Errorf("hits = %v, want lines 1 and 3", hits)
		}
	})
}

// hiTok is a github-token shape with entropy above the key-context floor, so
// key-context claims it too. ghTok's repeated A1b2 is under the floor.
func hiTok() string { return "ghp_" + "aB3dE5fG7hJ9" + "kL1mN2pQ4rS6" + "tU8vW0xY2zC4" }

func TestRedactOverlappingRulesReplaceOnce(t *testing.T) {
	line := "token = " + hiTok()
	rules := map[string]bool{}
	for _, h := range secretscan.ScanUnfiltered("probe", line) {
		rules[h.Rule] = true
	}
	if !rules["github-token"] || !rules["key-context"] {
		t.Fatalf("precondition: %q must be both a github-token and a key-context value, scans as %v", line, rules)
	}
	out, hits := redactClean(t, line)
	if out != "token = [redacted github-token]" {
		t.Errorf("out = %q", out)
	}
	if len(hits) != 1 || hits[0].Rule != "github-token" || hits[0].Length != len(hiTok()) {
		t.Errorf("hits = %v, want one github-token hit of the token's length", hits)
	}
	// A key-context value that starts before the token reaches it first, and
	// the merged span is named for it.
	out, hits = redactClean(t, "password = Zq9."+hiTok())
	if out != "password = [redacted key-context]" || len(hits) != 1 || hits[0].Rule != "key-context" {
		t.Errorf("out = %q, hits = %v; want one key-context redaction over the union", out, hits)
	}
}

// TestRedactReachesAFixedPoint: replacing a token inside a carved-out
// key-context value leaves a high-entropy remainder that matches on its own.
// A single pass would print it; Redact repeats until nothing changes.
func TestRedactReachesAFixedPoint(t *testing.T) {
	head := "Q7w!E9r#T2y%U4i^"
	in := "password = " + head + "." + hiTok() + "." + strings.Repeat("a", 200)
	out, hits := redactClean(t, in)
	if strings.Contains(out, head) || strings.Contains(out, hiTok()) {
		t.Errorf("a secret survives: %q", out)
	}
	if !strings.HasPrefix(out, "password = [redacted key-context][redacted github-token].") {
		t.Errorf("out = %q", out)
	}
	if len(hits) != 2 {
		t.Errorf("hits = %v, want the token and the remainder", hits)
	}
}

func TestRedactPEMWhole(t *testing.T) {
	body := []string{strings.Repeat("MIIE", 16), strings.Repeat("vQIB", 16), strings.Repeat("AAKC", 16)}
	end := "-----END " + "RSA PRIVATE KEY-----"

	t.Run("terminated", func(t *testing.T) {
		in := "key: " + pemHeader() + "\n" + strings.Join(body, "\n") + "\n" + end + " trailing\nafter " + ghTok() + "\nlast\n"
		out, hits := redactClean(t, in)
		for _, l := range body {
			if strings.Contains(out, l) {
				t.Errorf("a key line survives: %q", out)
			}
		}
		if out != "key: [redacted pem-private-key] trailing\nafter [redacted github-token]\nlast\n" {
			t.Errorf("out = %q", out)
		}
		if len(hits) != 2 || hits[0].Rule != "pem-private-key" || hits[0].Line != 1 || hits[1].Line != 6 {
			t.Errorf("hits = %v, want the pem on line 1 and the token on line 6", hits)
		}
	})

	t.Run("CRLF", func(t *testing.T) {
		in := pemHeader() + "\r\n" + strings.Join(body, "\r\n") + "\r\n" + end + "\r\nafter\r\n"
		if out, _ := redactClean(t, in); out != "[redacted pem-private-key]\r\nafter\r\n" {
			t.Errorf("out = %q", out)
		}
	})

	t.Run("one line", func(t *testing.T) {
		in := "a " + pemHeader() + body[0] + end + " b " + ghTok()
		if out, _ := redactClean(t, in); out != "a [redacted pem-private-key] b [redacted github-token]" {
			t.Errorf("out = %q", out)
		}
	})

	t.Run("unterminated runs to EOF", func(t *testing.T) {
		in := "before\n" + pemHeader() + "\n" + strings.Join(body, "\n") + "\nno end here\n"
		out, hits := redactClean(t, in)
		if out != "before\n[redacted pem-private-key]" {
			t.Errorf("out = %q", out)
		}
		if len(hits) != 1 || hits[0].Line != 2 || hits[0].Length != len(in)-len("before\n") {
			t.Errorf("hits = %v, want one pem hit on line 2 covering the rest", hits)
		}
	})

	t.Run("a second block", func(t *testing.T) {
		block := pemHeader() + "\n" + body[0] + "\n" + end
		if out, _ := redactClean(t, block+"\nmid\n"+block+"\n"); out != "[redacted pem-private-key]\nmid\n[redacted pem-private-key]\n" {
			t.Errorf("out = %q", out)
		}
	})
}

func TestRedactIgnoresAllowMarker(t *testing.T) {
	m := secretscan.AllowMarker
	out, hits := redactClean(t, ghTok()+" "+m+"\nquote: "+m+"\n")
	if strings.Contains(out, ghTok()) {
		t.Errorf("the marker kept the token: %q", out)
	}
	if strings.Contains(out, m) {
		t.Errorf("the marker survives into the output: %q", out)
	}
	if len(hits) != 1 {
		t.Errorf("hits = %v, want 1", hits)
	}
	if !strings.HasPrefix(out, "[redacted github-token] ") || strings.Count(out, "\n") != 2 {
		t.Errorf("out = %q, want the token redacted and both lines kept", out)
	}
}

func TestRedactCleanTextUntouched(t *testing.T) {
	cases := []string{
		`token = "$GITHUB_TOKEN"`,
		`api_key = "<your-token>"`,
		awsExample(),
		"password = " + strings.Repeat("a", 24),
		"line one\r\nline two\r\n",
		"no newline at the end",
		"",
	}
	for _, row := range benignCorpus() {
		cases = append(cases, row.line)
	}
	for _, in := range cases {
		out, hits := secretscan.Redact(in)
		if out != in || len(hits) != 0 {
			t.Errorf("Redact(%q) = %q, %v; want it back unchanged with no hits", in, out, hits)
		}
	}
}

func TestRedactKeepsKeyName(t *testing.T) {
	value := highEntropy()
	out, hits := redactClean(t, `api_key = "`+value+`"`)
	if out != `api_key = "[redacted key-context]"` {
		t.Errorf("out = %q", out)
	}
	if len(hits) != 1 || hits[0].Length != len(value) {
		t.Fatalf("hits = %v, want one hit of the value's length", hits)
	}
	h := hits[0]
	for _, field := range []string{h.Rule, h.Location, h.Prefix, h.String()} {
		if strings.Contains(field, value) {
			t.Errorf("a Hit field holds the value: %q", field)
		}
	}
}
