package cmd

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/pflag"
)

// optionsPrompt reads the /dross-options prompt, which is the only surface that
// claims to reach EVERY dross-managed setting.
func optionsPrompt(t *testing.T) string {
	t.Helper()
	root := repoRootForDocs(t)
	b, err := os.ReadFile(filepath.Join(root, "assets", "prompts", "options.md"))
	if err != nil {
		t.Fatalf("read options.md: %v", err)
	}
	return string(b)
}

// TestOptionsCoversEveryLocalKey walks localKeys itself rather than a hand-typed
// list, so a key added to the store without a line in the prompt fails here.
//
// The store is the case that motivated this test: .dross/local.toml is
// gitignored, absent from `dross project show`, and every value in it was
// written by a one-shot command. If the settings surface does not name a key,
// nothing else will, and the user finds out it exists by reading source.
func TestOptionsCoversEveryLocalKey(t *testing.T) {
	prompt := optionsPrompt(t)
	for key := range localKeys {
		if !strings.Contains(prompt, key) {
			t.Errorf("options.md does not mention the settable local key %q", key)
		}
	}
}

// TestOptionsCoversTheConsentVerbs pins the keys `dross local set` deliberately
// REFUSES.
//
// They are the ones a settings surface is most likely to miss, precisely
// because the generic key-writer cannot reach them — and the ones whose absence
// costs most, since a stale grant or a lapsed trust is silent until a verify
// refuses to run.
func TestOptionsCoversTheConsentVerbs(t *testing.T) {
	prompt := optionsPrompt(t)
	for _, want := range []string{
		"mutation remote grant",
		"mutation remote revoke",
		"mutation remote status",
		"dross trust",
		"trusted_test_command",
		"mutation_remote_host",
		// The lane grant is the newest member of the same family, and the
		// one a settings surface is most likely to miss: it is keyed per
		// lane, so a user who never opens local.toml has no way to learn
		// that renaming a lane silently untrusts it.
		"dross test lane add",
		"dross trust --lane",
		"trusted_lane_commands",
		// --selector changes what a consented lane spawns, so a settings
		// surface claiming to reach every dross-managed setting cannot omit
		// the one field that does that.
		"--selector",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("options.md does not mention %q", want)
		}
	}
	// ...and it must not tell the user to write a grant key through the generic
	// writer, which would be the consent_model decision inverted.
	for _, forbidden := range []string{
		"local set mutation_remote_host",
		"local set mutation_remote_workdir",
		"local set trusted_test_command",
		"local set trusted_lane_commands",
	} {
		if strings.Contains(prompt, forbidden) {
			t.Errorf("options.md tells the user to write a consent key via the generic key-writer: %q", forbidden)
		}
	}
}

// TestOptionsCoversEveryProjectSection is the drift guard for project.toml.
//
// options.md instructs the reader to STOP on a field it does not cover, calling
// that a schema-vs-prompt drift bug. That instruction was itself unenforced:
// [board] and [mutation] were printed by `dross project show` while the prompt
// named neither. This asserts the top-level sections; adding a new one to the
// Project struct without a section here fails.
func TestOptionsCoversEveryProjectSection(t *testing.T) {
	prompt := optionsPrompt(t)
	for _, section := range []string{
		"project.", "stack.", "runtime.", "repo.", "remote.",
		"board.", "mutation.", "paths.", "env.", "goals.",
		"constraints", "competition",
	} {
		if !strings.Contains(prompt, section) {
			t.Errorf("options.md does not cover the project.toml section %q", section)
		}
	}
}

// TestOptionsCoversTheProviderConditionalRemoteFields.
//
// These three are the fields a reader skims past because they are empty in the
// common case — and each one is load-bearing for exactly one provider, where a
// missing value produces an auth failure that names nothing useful.
func TestOptionsCoversTheProviderConditionalRemoteFields(t *testing.T) {
	prompt := optionsPrompt(t)
	for _, want := range []string{
		"remote.auth_user", "remote.auth_scheme", "remote.project_id",
		"board.auth_user", "board.github_project", "board.milestone_mode",
		"stack.profile",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("options.md does not mention %q", want)
		}
	}
}

// TestOptionsSaysMutationIsNotSettableThroughProjectSet.
//
// `dross project set mutation.adapters` fails — there is no dotted-path writer
// for [mutation]. A prompt that listed the field without that caveat would send
// the user into an error and leave the adapters allowlist unset, which is the
// state that makes `dross doctor` probe every toolchain and fail a Go-only repo
// over a missing npx.
func TestOptionsSaysMutationIsNotSettableThroughProjectSet(t *testing.T) {
	prompt := optionsPrompt(t)
	idx := strings.Index(prompt, "## 13. Mutation adapters")
	if idx < 0 {
		t.Fatal("options.md has no mutation adapters section")
	}
	section := prompt[idx:]
	if end := strings.Index(section[3:], "\n## "); end >= 0 {
		section = section[:end+3]
	}
	if !strings.Contains(section, "not settable") {
		t.Error("the mutation section does not say the fields are unsettable through `dross project set`")
	}
	if !strings.Contains(section, "mutation.adapters") {
		t.Error("the mutation section does not name mutation.adapters")
	}
}

// TestOptionsSectionNumbersAreContiguous keeps the numbering honest: the prompt
// cross-references sections by number ("see §11", "see §14"), and a duplicated
// or skipped heading silently points the reader at the wrong one.
func TestOptionsSectionNumbersAreContiguous(t *testing.T) {
	prompt := optionsPrompt(t)
	seen := 0
	for _, line := range strings.Split(prompt, "\n") {
		if !strings.HasPrefix(line, "## ") {
			continue
		}
		rest := strings.TrimPrefix(line, "## ")
		dot := strings.Index(rest, ".")
		if dot <= 0 {
			continue // "## Section pick" and friends carry no number
		}
		n, err := strconv.Atoi(rest[:dot])
		if err != nil {
			continue
		}
		if n != seen {
			t.Errorf("section heading %q breaks the sequence: expected %d", line, seen)
		}
		seen = n + 1
	}
	if seen < 17 {
		t.Errorf("options.md has only %d numbered sections; the settings surface lost one", seen)
	}
}

// TestReadmeDocumentsTestLanes is the documentation half of the lane surface,
// on the TestDocsCoverAllowHosts precedent: a flag nobody can find is a flag
// that does not exist.
//
// It names the two refusal exit codes as well as the verbs. Those are the
// codes a caller reads when a run did NOT happen, and an agent that treats an
// undocumented non-zero as a red suite is one commit away from the false-green
// the whole exit contract exists to prevent.
func TestReadmeDocumentsTestLanes(t *testing.T) {
	root := repoRootForDocs(t)
	b, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		t.Fatalf("read README.md: %v", err)
	}
	readme := string(b)
	for _, want := range []string{
		"dross test --files",
		"dross test lane add",
		"dross test lane remove",
		"runtime.test_lane",
		"dross trust --lane",
		// The pre-selector wording, kept intact: it is still one of the two
		// ways a run measures nothing, and an agent reading only the new
		// clause would have no name for the plain no-lane case.
		"matched no lane",
		// The selector surface. A shipped flag with no README line is a flag
		// nobody can find, which is the same rule `dross test --files`
		// already answers to.
		"--selector",
		"go-package",
		"--empty-exit",
		"selector miss",
		// The prepare surface. A bootstrap line is code the repo asks this
		// machine to run before its tests, so a reader who cannot find it
		// documented cannot know it exists to review.
		"--prepare",
		"dross test lane edit",
	} {
		if !strings.Contains(readme, want) {
			t.Errorf("README.md does not document %q", want)
		}
	}
	// Exit 5 now has two causes, and an agent that only ever learned the first
	// reads an all-miss run as an unexplained non-zero — which is exactly the
	// state the exit contract exists to keep it out of.
	if !strings.Contains(readme, "every matched lane's selector collected nothing") {
		t.Error("README.md's exit-5 contract does not cover the all-miss run")
	}
	// Exit 7 is documented AND placed. The position is the contract: an agent
	// that read 7 as ranking below a red suite would commit on a run where a
	// lane never got as far as being measured.
	if !strings.Contains(readme, "`7`") {
		t.Fatal("README.md's `dross test` row does not name exit 7")
	}
	order := []string{"transport", "partial", "prepare", "red", "refused", "nothing-measured"}
	at := -1
	for _, name := range order {
		i := strings.Index(readme, "> "+name)
		if name == "transport" {
			i = strings.Index(readme, "mislead: transport")
		}
		if i < 0 {
			t.Fatalf("README.md's stated precedence order omits %q", name)
		}
		if i <= at {
			t.Errorf("README.md states %q out of precedence order", name)
		}
		at = i
	}
}

// TestOptionsDocumentsTheSelectorSurfaceCorrectly guards the two ways the
// settings prompt could describe this surface wrongly: by sending the reader to
// hand-edit project.toml, or by advertising a flag the command does not
// register.
func TestOptionsDocumentsTheSelectorSurfaceCorrectly(t *testing.T) {
	prompt := optionsPrompt(t)

	// Editing stays remove-then-re-add for every field but prepare (locked
	// lane_edit_surface, narrowed by lane-prepare-step). A prompt that told
	// the reader to open project.toml would route around every refusal
	// `dross test lane add` performs before the write.
	if !strings.Contains(prompt, "remove-then-re-add") {
		t.Error("options.md does not route lane edits through remove-then-re-add")
	}
	// The one exception, named as one. A prompt that advertised the edit verb
	// without saying it is prepare-only would send the reader to a refusal for
	// every other field.
	if !strings.Contains(prompt, "dross test lane edit") {
		t.Error("options.md does not name `dross test lane edit --prepare`")
	}
	for _, field := range []string{"match", "command", "selector", "empty_exit"} {
		if !strings.Contains(prompt, field) {
			t.Errorf("options.md no longer says which fields stay remove-then-re-add: %q is missing", field)
		}
	}
	for _, forbidden := range []string{
		"hand-edit runtime.test_lane",
		"edit the [[runtime.test_lane]] block",
		"edit runtime.test_lane",
	} {
		if strings.Contains(prompt, forbidden) {
			t.Errorf("options.md tells the reader to hand-edit a lane: %q", forbidden)
		}
	}

	// Every --flag advertised for `dross test lane add`, in options.md AND in
	// the README, has to be one the cobra command actually registers. A doc
	// naming --selector-template is a doc that sends the user to a refusal.
	registered := map[string]bool{}
	testLaneAdd().Flags().VisitAll(func(f *pflag.Flag) { registered[f.Name] = true })

	root := repoRootForDocs(t)
	readmeBytes, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	flagRe := regexp.MustCompile(`dross test lane add[^\n` + "`" + `]*`)
	for _, doc := range []struct{ name, body string }{
		{"options.md", prompt},
		{"README.md", string(readmeBytes)},
	} {
		for _, example := range flagRe.FindAllString(doc.body, -1) {
			for _, tok := range strings.Fields(example) {
				if !strings.HasPrefix(tok, "--") {
					continue
				}
				name := strings.TrimPrefix(strings.SplitN(tok, "=", 2)[0], "--")
				name = strings.Trim(name, "`*_,.")
				if name == "" {
					continue
				}
				if !registered[name] {
					t.Errorf("%s advertises `dross test lane add --%s`, which the command does not register (in %q)", doc.name, name, example)
				}
			}
		}
	}
}

// TestReadmeDocumentsDetachedRuns: a shipped verb with no README line is a verb
// nobody can find, which is the rule `dross test --files` already answers to.
//
// The exit codes matter most. `verify results` reports five states through five
// codes precisely so a caller can poll without parsing prose, and a contract
// nobody wrote down is one every caller re-derives differently — the same
// argument the `dross test` exit table already carries.
func TestReadmeDocumentsDetachedRuns(t *testing.T) {
	root := repoRootForDocs(t)
	b, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		t.Fatalf("read README.md: %v", err)
	}
	readme := string(b)
	for _, want := range []string{
		"--detach",
		"dross verify results",
		"dross verify status",
		"--cancel",
		// The scheduling half. An --at with no documented meaning leaves the
		// reader guessing whose clock it is on, which is the one thing that
		// decides whether an off-hours run lands off-hours.
		"--at",
		"host's clock",
		// The provenance rule the whole fetch rests on.
		"recorded at dispatch",
	} {
		if !strings.Contains(readme, want) {
			t.Errorf("README.md does not document %q", want)
		}
	}
	// The states are the contract a poller reads. Documented as a set, because
	// a reader who learned only "finished" treats every other code as failure —
	// including the two that mean the run is alive and fine.
	for _, state := range []string{"scheduled", "running", "finished", "unreachable", "gone"} {
		if !strings.Contains(readme, state) {
			t.Errorf("README.md does not document the %q result state", state)
		}
	}
}

// TestVerifyPromptTeachesTheDetachedPath: the prompt is what an agent reads
// before deciding how to run a two-hour leg. One that only describes the
// attached form will keep holding a session for the length of the run, which is
// the entire problem this phase exists to remove — the feature would ship and
// never be used.
func TestVerifyPromptTeachesTheDetachedPath(t *testing.T) {
	root := repoRootForDocs(t)
	b, err := os.ReadFile(filepath.Join(root, "assets", "prompts", "verify.md"))
	if err != nil {
		t.Fatalf("read assets/prompts/verify.md: %v", err)
	}
	prompt := string(b)
	for _, want := range []string{
		"--detach",
		"dross verify results",
		"dross verify status",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("assets/prompts/verify.md does not name %q", want)
		}
	}
	// The exit codes that mean "no verdict was produced" must be called out,
	// or an agent reads a still-running run as a failed one and sends the user
	// to fix code that was never measured — the same false-red the `dross test`
	// exit contract exists to prevent.
	if !strings.Contains(prompt, "did not report") && !strings.Contains(prompt, "not a verdict") {
		t.Error("assets/prompts/verify.md does not tell the agent that a non-finished " +
			"result state is not a verdict about the code")
	}
}
