package cmd

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// The c-3 regression guard. tool-output-not-persisted removed a stryker.go
// branch that tee'd the tool's stream into a bounded head buffer and then
// quoted that head into returned errors. The residual AST ban in
// internal/mutation cannot see it — the head passes through a strings.Builder
// and a method before reaching the error — so the taint engine has to. This
// fixture is that branch, copied verbatim from 6f27eaa^, and it must keep
// tripping at all three of its escapes.

// strykerPrePhasePins are the lines of `git show 6f27eaa^:internal/mutation/
// stryker.go` the fixture must carry byte for byte: the tee (115-118), the
// reportless message and return (142, 150), headBuffer and quote() (176-222)
// and checkInstrumented's return (315). Held here, not read from git, so the
// pin needs no .git on the remote runner.
var strykerPrePhasePins = map[string]string{
	"tee (115-118)":                  "\thead := &headBuffer{limit: strykerHeadBytes}\n\tsink := io.MultiWriter(os.Stderr, head)\n\tcmd.Stdout = sink\n\tcmd.Stderr = sink",
	"reportless msg (142)":           "\t\t\tmsg := fmt.Sprintf(\"stryker did not write a report at %s.\\n%s\", reportPath, head.quote(strykerHeadLines))",
	"reportless return (150)":        "\t\t\treturn nil, errors.New(msg)",
	"headBuffer (176-197)":           "\n// headBuffer retains the first `limit` bytes written through it and silently\n// discards the rest, always reporting a full write so it can sit inside an\n// io.MultiWriter without truncating the stream its sibling is rendering.\ntype headBuffer struct {\n\tlimit int\n\tbuf   bytes.Buffer\n}\n\nfunc (h *headBuffer) Write(p []byte) (int, error) {\n\tif room := h.limit - h.buf.Len(); room > 0 {\n\t\tif len(p) <= room {\n\t\t\th.buf.Write(p)\n\t\t} else {\n\t\t\th.buf.Write(p[:room])\n\t\t}\n\t}\n\t// len(p), never the amount kept: a short count is an io.ErrShortWrite to\n\t// io.MultiWriter, which would abort the write to os.Stderr as well and\n\t// truncate the live output the moment the cap was reached.\n\treturn len(p), nil\n}",
	"quote (201-222)":                "func (h *headBuffer) quote(n int) string {\n\ttext := strings.TrimRight(h.buf.String(), \"\\n\")\n\tif text == \"\" {\n\t\treturn \"stryker produced no output at all \u2014 it may not have started.\"\n\t}\n\tlines := strings.Split(text, \"\\n\")\n\ttruncated := false\n\tif len(lines) > n {\n\t\tlines, truncated = lines[:n], true\n\t}\n\tvar b strings.Builder\n\tb.WriteString(\"the head of stryker's output, which is where the cause is:\\n\\n\")\n\tfor _, l := range lines {\n\t\tb.WriteString(\"    \")\n\t\tb.WriteString(l)\n\t\tb.WriteString(\"\\n\")\n\t}\n\tif truncated {\n\t\tb.WriteString(\"    \u2026 (output continues above)\\n\")\n\t}\n\treturn b.String()\n}",
	"checkInstrumented return (315)": "\treturn fmt.Errorf(\"%s\\n%s\", msg, head.quote(strykerHeadLines))",
}

// TestStrykerPrePhaseBranchIsPinned: a fixture edited toward a shape the
// engine happens to catch would prove nothing about the real one.
func TestStrykerPrePhaseBranchIsPinned(t *testing.T) {
	body, err := os.ReadFile(fixturePath("taint", "stryker_prephase.go.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for name, pin := range strykerPrePhasePins {
		if !strings.Contains(string(body), pin) {
			t.Errorf("stryker_prephase.go.txt no longer carries the pre-phase %s verbatim:\n%s", name, pin)
		}
	}
}

// strykerFixture loads the fixture with optional rewrites, each of which must
// match.
func strykerFixture(t *testing.T, rewrites ...[2]string) (*ssaFixture, string) {
	t.Helper()
	body, err := os.ReadFile(fixturePath("taint", "stryker_prephase.go.txt"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(body)
	for _, r := range rewrites {
		if !strings.Contains(src, r[0]) {
			t.Fatalf("rewrite never matched %q", r[0])
		}
		src = strings.Replace(src, r[0], r[1], 1)
	}
	fx, err := typecheckFixture(liveImportable(t), []fixtureSource{{Name: "stryker_prephase.go.txt", Src: []byte(src)}})
	if err != nil {
		t.Fatal(err)
	}
	return fx, src
}

// lineOf is the 1-based line of the first occurrence of needle in src.
func lineOf(t *testing.T, src, needle string) int {
	t.Helper()
	i := strings.Index(src, needle)
	if i < 0 {
		t.Fatalf("fixture does not contain %q", needle)
	}
	return strings.Count(src[:i], "\n") + 1
}

// TestStrykerPrePhaseBranchTrips: the reportless errors.New(msg),
// checkInstrumented's Errorf and the two-line paraphrase each escape — and
// each names the `cmd.Stdout = sink` store as its origin.
func TestStrykerPrePhaseBranchTrips(t *testing.T) {
	fx, src := strykerFixture(t)
	taint, _ := execTaintScan(fx.srcView)
	assertCorpus(t, fx, "taint", taintHits(taint))
	stdout := lineOf(t, src, "cmd.Stdout = sink")
	for _, f := range taint {
		named := false
		for _, o := range f.Origins {
			if o.Line == stdout {
				named = true
			}
		}
		if !named {
			t.Errorf("%s does not name the cmd.Stdout = sink origin (line %d)", f, stdout)
		}
	}
}

// TestStrykerTwinsStayClean: the engine does not over-trip. Swapping the
// reportless error for printHead(os.Stderr, …) plus fixed prose, or the
// paraphrase's &quoted for os.Stderr, leaves no finding at the swapped site.
func TestStrykerTwinsStayClean(t *testing.T) {
	for _, tc := range []struct {
		name    string
		rewrite [2]string
		site    string
	}{
		{"head to stderr, fixed prose",
			[2]string{"\t\t\treturn nil, errors.New(msg) // WANT taint",
				"\t\t\tprintHead(os.Stderr, head)\n\t\t\t_ = msg\n\t\t\treturn nil, errors.New(\"stryker wrote no report; its output is above\")"},
			"errors.New(\"stryker wrote no report"},
		{"quote to stderr",
			[2]string{"fmt.Fprint(&quoted, head.quote(strykerHeadLines))", "fmt.Fprint(os.Stderr, head.quote(strykerHeadLines))"},
			"return fmt.Errorf(\"stryker failed:"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fx, src := strykerFixture(t, tc.rewrite)
			site := lineOf(t, src, tc.site)
			taint, _ := execTaintScan(fx.srcView)
			for _, f := range taint {
				if f.Escape.Line == site {
					t.Errorf("the twin still trips at line %d: %s", site, f)
				}
			}
			if len(taint) == 0 {
				t.Error("the twin produced no finding at all — the other escapes stopped tripping, so this proves nothing")
			}
		})
	}
	_ = fmt.Sprint
}
