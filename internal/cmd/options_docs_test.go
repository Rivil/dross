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
		// The template surface. It reaches runners the closed selector enum
		// cannot shape, and it is the one scoping field that changes the
		// consent line — a reader who cannot find it documented cannot know
		// why their granted lane went stale.
		"--selector-template",
		"--selector-join",
		"{path}",
		"{paths}",
	} {
		if !strings.Contains(readme, want) {
			t.Errorf("README.md does not document %q", want)
		}
	}
	// The c-4 warning, in the README as well as at the two surfaces that print
	// it. It describes a lane that VALIDATES and still measures the whole
	// suite, so a reader who never meets the wording has no name for the one
	// failure a green scoped run can hide.
	for _, want := range []string{"./...", "runs the union"} {
		if !strings.Contains(readme, want) {
			t.Errorf("README.md does not document the whole-tree warning: %q missing", want)
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
//
// The remove-then-re-add assertion is RE-POINTED rather than dropped. It used
// to require the prompt to route match, command, selector and empty_exit
// through remove-then-re-add; lane-selector-custom made all four editable in
// place, so a doc still saying that sends the reader to a workaround that
// DROPS the lane's grant. What the guard protects is unchanged — a doc must
// never route a lane change around the CLI's refusals — so it now requires the
// prompt to name the in-place field set, and still refuses any instruction to
// open the [[runtime.test_lane]] block by hand.
func TestOptionsDocumentsTheSelectorSurfaceCorrectly(t *testing.T) {
	prompt := optionsPrompt(t)

	// Only the lane's NAME is remove-then-re-add now, and the prompt has to
	// say so: it keys the consent store, so it cannot move without the grant
	// moving with it.
	if !strings.Contains(prompt, "remove-then-re-add") {
		t.Error("options.md no longer says which lane change is remove-then-re-add")
	}
	if !strings.Contains(prompt, "dross test lane edit") {
		t.Error("options.md does not name `dross test lane edit`")
	}
	// Every field the edit verb writes must be named. A prompt that omitted
	// one would leave the reader believing that field still needs the
	// grant-dropping workaround.
	for _, field := range []string{
		"--match", "--command", "--prepare", "--selector",
		"--selector-template", "--selector-join", "--empty-exit",
		"--toolchain", "--install",
	} {
		if !strings.Contains(prompt, field) {
			t.Errorf("options.md does not name the in-place-editable field %q", field)
		}
	}
	// The claim the phase falsified must be gone: a SENTENCE that routes one
	// of those four fields through remove-then-re-add is the failure this
	// guard was re-pointed to catch. Sentences, not lines — a markdown
	// paragraph is one line, and this one legitimately says "a blank command"
	// and "an empty match list" describing what the refusal gate catches.
	//
	// Word-bounded, so `trusted_lane_commands` is not read as the word
	// "command": the key name has to appear in the very sentence that explains
	// why the lane's NAME is the exception.
	editable := regexp.MustCompile(`\b(match|command|selector|empty_exit)\b`)
	namesTheException := false
	for _, sentence := range strings.Split(prompt, ". ") {
		if !strings.Contains(sentence, "remove-then-re-add") {
			continue
		}
		if got := editable.FindString(sentence); got != "" {
			t.Errorf("options.md still routes %s through remove-then-re-add: %q", got, sentence)
		}
		if regexp.MustCompile(`\bname\b`).MatchString(sentence) {
			namesTheException = true
		}
	}
	if !namesTheException {
		t.Error("options.md does not say that the lane's NAME is the one field remove-then-re-add still applies to")
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

	// Every --flag advertised for a lane verb, in options.md AND in the
	// README, has to be one the cobra command actually registers. A doc naming
	// a flag that does not exist sends the user to a refusal.
	//
	// Both verbs are parsed, not just `lane add`: the edit verb now carries the
	// larger flag set, so a guard reading only the add examples would leave the
	// half most likely to drift unchecked.
	addFlags := map[string]bool{}
	testLaneAdd().Flags().VisitAll(func(f *pflag.Flag) { addFlags[f.Name] = true })
	editFlags := map[string]bool{}
	testLaneEdit().Flags().VisitAll(func(f *pflag.Flag) { editFlags[f.Name] = true })

	root := repoRootForDocs(t)
	readmeBytes, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	docs := []struct{ name, body string }{
		{"options.md", prompt},
		{"README.md", string(readmeBytes)},
	}
	for _, verb := range []struct {
		name       string
		registered map[string]bool
	}{
		{"dross test lane add", addFlags},
		{"dross test lane edit", editFlags},
	} {
		flagRe := regexp.MustCompile(regexp.QuoteMeta(verb.name) + `[^\n` + "`" + `]*`)
		checked := 0
		for _, doc := range docs {
			for _, example := range flagRe.FindAllString(doc.body, -1) {
				for _, tok := range strings.Fields(example) {
					if !strings.HasPrefix(tok, "--") {
						continue
					}
					name := strings.TrimPrefix(strings.SplitN(tok, "=", 2)[0], "--")
					name = strings.Trim(name, "`*_,.|")
					if name == "" {
						continue
					}
					checked++
					if !verb.registered[name] {
						t.Errorf("%s advertises `%s --%s`, which the command does not register (in %q)", doc.name, verb.name, name, example)
					}
				}
			}
		}
		if checked == 0 {
			t.Errorf("no `%s` flag example was found in either doc — the guard is vacuous for that verb", verb.name)
		}
	}
}
