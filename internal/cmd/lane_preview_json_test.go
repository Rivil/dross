package cmd

// `dross test lane preview --json`.
//
// The recurring assertion is that the payload and the transcript carry the SAME
// facts in the SAME words. Each checked against its own literal would pass while
// the two described different runs, and a consumer reading the JSON would be
// looking at a second answer nothing validates.

import (
	"encoding/json"
	"strings"
	"testing"
)

// previewJSON runs the verb with --json and returns the parsed document,
// asserting the bare-document shape on the way through.
func previewJSON(t *testing.T, args ...string) map[string]any {
	t.Helper()
	var out string
	if err := runCmdCapturing(t, &out, Test(), append([]string{"lane", "preview", "--json"}, args...)...); err != nil {
		t.Fatalf("dross test lane preview --json %v: %v", args, err)
	}
	return assertBareJSONDocument(t, "lane preview --json", out)
}

// jsonStrings pulls a []string field off a decoded document.
func jsonStrings(doc map[string]any, key string) []string {
	raw, _ := doc[key].([]any)
	out := []string{}
	for _, v := range raw {
		s, _ := v.(string)
		out = append(out, s)
	}
	return out
}

// jsonLanes pulls the lanes array off a decoded document.
func jsonLanes(t *testing.T, doc map[string]any) []map[string]any {
	t.Helper()
	raw, ok := doc["lanes"].([]any)
	if !ok {
		t.Fatalf("lanes is not an array: %#v", doc["lanes"])
	}
	var out []map[string]any
	for _, v := range raw {
		m, ok := v.(map[string]any)
		if !ok {
			t.Fatalf("a lanes entry is not an object: %#v", v)
		}
		out = append(out, m)
	}
	return out
}

// TestPreviewJSONIsBareAndCarriesTheFindings is c-5: the payload holds the same
// facts the transcript printed, in a stable machine-readable shape.
//
// The `# <path>` header every `show --json` drops is checked here too, because
// a `#` line is not JSON and anything downstream would have to strip it before
// parsing — which is the one thing --json exists to avoid.
func TestPreviewJSONIsBareAndCarriesTheFindings(t *testing.T) {
	filesFixture(t, selectorLanes)
	grantAllLanes(t)
	touchFile(t, "internal/cmd/here.go")
	args := []string{
		"--files", "internal/cmd/here.go",
		"--files", "internal/gone/away.go",
		"--files", "NOTES.txt",
		"--files", "/abs/x.go",
		"--no-probe",
	}

	doc := previewJSON(t, args...)
	lanes := jsonLanes(t, doc)
	if len(lanes) != 1 {
		t.Fatalf("want 1 lane in the payload, got %d", len(lanes))
	}

	// The transcript, over the same argv: every value below is compared
	// against what was PRINTED, never against a literal of its own.
	out := previewOut(t, args...)
	line, _ := lanes[0]["line"].(string)
	if line == "" || !strings.Contains(out, line) {
		t.Errorf("lanes[0].line = %q, which the transcript does not carry:\n%s", line, out)
	}
	if got := jsonStrings(lanes[0], "dropped"); len(got) != 1 || got[0] != "internal/gone/away.go" {
		t.Errorf("dropped = %v, want [internal/gone/away.go]", got)
	}
	if got := jsonStrings(doc, "unmatched"); len(got) != 1 || got[0] != "NOTES.txt" {
		t.Errorf("unmatched = %v, want [NOTES.txt]", got)
	}
	if got := jsonStrings(doc, "out_of_tree"); len(got) != 1 || got[0] != "/abs/x.go" {
		t.Errorf("out_of_tree = %v, want [/abs/x.go]", got)
	}
}

// TestPreviewJSONUsesItsOwnFlagUsage: jsonFlagUsage promises "instead of toml",
// and preview has no toml rendering to be instead of.
//
// A usage line naming a format the command cannot emit is a small lie in
// --help, and --help is the only place a reader finds out what --json does.
func TestPreviewJSONUsesItsOwnFlagUsage(t *testing.T) {
	cmd, _, err := Test().Find([]string{"lane", "preview"})
	if err != nil {
		t.Fatal(err)
	}
	flag := cmd.Flags().Lookup("json")
	if flag == nil {
		t.Fatal("preview registers no --json flag")
	}
	if flag.Usage == jsonFlagUsage {
		t.Errorf("preview registers jsonFlagUsage, which promises %q", jsonFlagUsage)
	}
	if strings.Contains(flag.Usage, "toml") {
		t.Errorf("the usage names toml, which preview never emits: %q", flag.Usage)
	}
}

// TestPreviewJSONReportsAnEmptyMatchWithoutFailing: an empty answer is an
// answer.
//
// `lanes` must be an empty ARRAY rather than null or absent, so a consumer can
// range over it without a null check — and the exit must stay 0, which is
// locked preview_exit_status seen from the machine-readable side.
func TestPreviewJSONReportsAnEmptyMatchWithoutFailing(t *testing.T) {
	filesFixture(t, selectorLanes)
	grantAllLanes(t)

	var out string
	if err := runCmdCapturing(t, &out, Test(), "lane", "preview", "--json", "--no-probe", "--files", "NOTES.txt"); err != nil {
		t.Fatalf("an unmatched-only set failed: %v", err)
	}
	doc := assertBareJSONDocument(t, "empty match", out)
	if _, ok := doc["lanes"].([]any); !ok {
		t.Errorf("lanes is %#v, want an empty array a consumer can range over", doc["lanes"])
	}
	if got := jsonStrings(doc, "unmatched"); len(got) != 1 || got[0] != "NOTES.txt" {
		t.Errorf("unmatched = %v, want [NOTES.txt]", got)
	}
	// A bare `{}` would satisfy "parses as an object" and carry nothing.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		t.Fatal(err)
	}
	if len(raw) < 3 {
		t.Errorf("the payload has %d field(s) — an empty match must still report the findings:\n%s", len(raw), out)
	}
}

// TestJSONCarriesConsentAndLocality: one vocabulary, two renderings.
//
// The strings are compared against the TRANSCRIPT rather than against literals,
// because a payload emitting "ok" where the transcript said "granted" would
// satisfy any literal assertion and still be a second answer.
func TestJSONCarriesConsentAndLocality(t *testing.T) {
	grantedLaneFixture(t, goAndWebLanes)
	installLaneLookPath(t)
	transportProbe(t)
	args := []string{"--files", "internal/a.go", "--files", "web/app.ts"}

	doc := previewJSON(t, args...)
	out := previewOut(t, args...)

	lanes := jsonLanes(t, doc)
	if len(lanes) != 2 {
		t.Fatalf("want 2 lanes in the payload, got %d", len(lanes))
	}
	for _, l := range lanes {
		name, _ := l["name"].(string)
		block := previewLaneBlock(out, name)
		consent, _ := l["consent"].(string)
		if consent == "" || !strings.Contains(block, "consent: "+consent) {
			t.Errorf("lane %s: consent %q is not the word the transcript used:\n%s", name, consent, block)
		}
		locality, _ := l["locality"].(string)
		if locality == "" || !strings.Contains(block, "runs on: "+locality) {
			t.Errorf("lane %s: locality %q is not the string the transcript printed:\n%s", name, locality, block)
		}
	}
	if got, _ := doc["host_state"].(string); got != string(hostUnresolved) {
		t.Errorf("host_state = %q, want %q", got, hostUnresolved)
	}
}
