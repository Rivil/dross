package review

import (
	"slices"
	"strings"
	"testing"

	"github.com/Rivil/dross/assets"
)

// writerDef returns the shipped writer definition's frontmatter (key → raw
// value) and body, read from the embedded assets the binary installs.
func writerDef(t *testing.T) (map[string]string, string) {
	t.Helper()
	b, err := assets.FS.ReadFile("agents/" + WriterAgent + ".md")
	if err != nil {
		t.Fatalf("the writer definition is not embedded: %v", err)
	}
	s := string(b)
	if !strings.HasPrefix(s, "---\n") {
		t.Fatal("the writer definition has no frontmatter")
	}
	end := strings.Index(s[4:], "\n---\n")
	if end < 0 {
		t.Fatal("the writer definition's frontmatter is never closed")
	}
	front := map[string]string{}
	for _, line := range strings.Split(s[4:4+end], "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok || strings.HasPrefix(line, " ") {
			continue
		}
		front[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	return front, s[4+end+5:]
}

// TestWriterDefinitionShape pins writer_spawn and c-1: the session's model,
// no auto-memory, and exactly the tools to read, edit and run scoped tests —
// never a spawn, the web or notebooks.
func TestWriterDefinitionShape(t *testing.T) {
	front, _ := writerDef(t)
	for _, key := range []string{"model", "memory", "omitClaudeMd"} {
		if _, ok := front[key]; ok {
			t.Errorf("the definition sets %s: the writer inherits the session's model and instructions, with no memory", key)
		}
	}
	tools, ok := front["tools"]
	if !ok || strings.TrimSpace(tools) == "" {
		t.Fatal("the definition sets no tools, so the writer inherits every tool")
	}
	var got []string
	for _, tool := range strings.Split(tools, ",") {
		got = append(got, strings.TrimSpace(tool))
	}
	slices.Sort(got)
	want := []string{"Bash", "Edit", "Glob", "Grep", "Read", "Write"}
	if !slices.Equal(got, want) {
		t.Errorf("tools = %v, want exactly %v", got, want)
	}
	for _, banned := range []string{"Agent", "Task", "WebFetch", "WebSearch", "NotebookEdit"} {
		if slices.Contains(got, banned) {
			t.Errorf("the writer's tools include %s", banned)
		}
	}
}

func TestWriterNameMatchesConst(t *testing.T) {
	front, _ := writerDef(t)
	if front["name"] != WriterAgent {
		t.Fatalf("definition name %q != review.WriterAgent %q: the gate would claim no spawn of it", front["name"], WriterAgent)
	}
}

// TestWriterNeverCommitsOrDecides pins c-1's "never commits, sets task status
// or finalizes": the body forbids every verb that would.
func TestWriterNeverCommitsOrDecides(t *testing.T) {
	_, body := writerDef(t)
	for _, want := range []string{
		"Never `git add`, `git commit`, `git stash`",
		"Never `dross task status`, `dross changes record`",
		"`dross phase`, `dross ship`, `dross issue`",
		"Never edit anything under `.dross/`.",
		"Edit or create **only the files the task record lists**",
		"Tests only as `dross test --files <path>`",
		"Never spawn another agent.",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the body no longer says %q", want)
		}
	}
}

// TestWriterReportBounded pins c-2's bounded report: files, test outcome and
// short notes in one fenced block, with no code and no diff.
func TestWriterReportBounded(t *testing.T) {
	_, body := writerDef(t)
	for _, want := range []string{
		"exactly one fenced block whose info string is `" + WriterReportFence + "`",
		"- `files` —",
		"- `test` — exactly `green`, or `red: <first failing test>`",
		"- `notes` — at most 5 lines",
		"No code, no diff and no file contents in the report",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the body no longer says %q", want)
		}
	}
	open := "```" + WriterReportFence + "\n"
	i := strings.Index(body, open)
	if i < 0 {
		t.Fatal("the body carries no worked dross-writer-report example")
	}
	example, _, ok := strings.Cut(body[i+len(open):], "```")
	if !ok {
		t.Fatal("the worked example is never closed")
	}
	var keys []string
	for _, line := range strings.Split(strings.TrimSpace(example), "\n") {
		k, _, _ := strings.Cut(line, ":")
		keys = append(keys, k)
	}
	if !slices.Equal(keys, []string{"files", "test", "notes"}) {
		t.Errorf("the worked example's keys are %v, want files, test, notes", keys)
	}
}

// TestWriterPromptLineShape: the spawn prompt is one line naming exactly the
// phase and task, so the gate can match it byte for byte.
func TestWriterPromptLineShape(t *testing.T) {
	p := WriterPromptLine("solo-task-subagent", "t-3")
	if strings.ContainsAny(p, "\r\n") {
		t.Fatalf("prompt line spans lines: %q", p)
	}
	for _, want := range []string{"t-3", "solo-task-subagent", "`dross task show solo-task-subagent t-3`"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt line %q does not name %q", p, want)
		}
	}
	if WriterPromptLine("solo-task-subagent", "t-4") == p || WriterPromptLine("other", "t-3") == p {
		t.Error("prompt lines for different tasks or phases collide")
	}
}
