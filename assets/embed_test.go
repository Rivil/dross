package assets

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// embeddedMarkdown walks FS and returns every embedded *.md path (slash-separated,
// rooted at commands/ or prompts/).
func embeddedMarkdown(t *testing.T) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	for _, root := range []string{"commands", "prompts"} {
		err := fs.WalkDir(FS, root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(p, ".md") {
				return nil
			}
			b, err := FS.ReadFile(p)
			if err != nil {
				return err
			}
			out[p] = b
			return nil
		})
		if err != nil {
			t.Fatalf("walk embedded %s: %v", root, err)
		}
	}
	return out
}

// onDiskMarkdown walks the assets/commands and assets/prompts directories on disk
// (the package dir is the test's working directory) and returns every *.md path
// using the same slash-separated, root-relative keys as embeddedMarkdown.
func onDiskMarkdown(t *testing.T) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	for _, root := range []string{"commands", "prompts"} {
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(p, ".md") {
				return nil
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			out[filepath.ToSlash(p)] = b
			return nil
		})
		if err != nil {
			t.Fatalf("walk on-disk %s: %v", root, err)
		}
	}
	return out
}

func keys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TestEmbedDrift is c-1's guard: the binary's embedded command skills and prompts
// must match the on-disk assets/ tree exactly — same file set, same bytes.
//
//   - Set equality (both directions) catches a drifted embed pattern: add or
//     rename a commands/dross-foo.md or a prompt on disk that the directive no
//     longer captures (e.g. dropping the `all:` prefix excludes _interaction.md,
//     narrowing to a glob misses an oddly-named file) and the missing path is
//     named here.
//   - Content equality catches an embedded copy that diverged from disk.
func TestEmbedDrift(t *testing.T) {
	embedded := embeddedMarkdown(t)
	onDisk := onDiskMarkdown(t)

	if len(onDisk) == 0 {
		t.Fatal("found no on-disk .md assets — test wiring is broken")
	}

	// Set equality, both directions, naming the drifted path.
	for _, p := range keys(onDisk) {
		if _, ok := embedded[p]; !ok {
			t.Errorf("on-disk asset not embedded: %s (embed pattern dropped it — check the `all:` prefix / glob)", p)
		}
	}
	for _, p := range keys(embedded) {
		if _, ok := onDisk[p]; !ok {
			t.Errorf("embedded asset missing on disk: %s (stale embed — file was deleted/renamed on disk)", p)
		}
	}

	// Content equality for the files present in both.
	for _, p := range keys(onDisk) {
		eb, ok := embedded[p]
		if !ok {
			continue
		}
		if string(eb) != string(onDisk[p]) {
			t.Errorf("embedded bytes differ from on-disk for %s", p)
		}
	}
}

// TestInteractionPlaybookFromFS proves the re-derived InteractionPlaybook resolves
// through FS to prompts/_interaction.md verbatim — the underscore-prefixed file
// that only the `all:` embed prefix preserves.
func TestInteractionPlaybookFromFS(t *testing.T) {
	want, err := os.ReadFile(filepath.Join("prompts", "_interaction.md"))
	if err != nil {
		t.Fatal(err)
	}
	if InteractionPlaybook != string(want) {
		t.Errorf("InteractionPlaybook does not match prompts/_interaction.md on disk")
	}
	if !strings.Contains(InteractionPlaybook, "Interaction playbook") {
		t.Errorf("InteractionPlaybook looks empty/wrong: %.40q", InteractionPlaybook)
	}
}

// TestMustReadStringPanicsOnMissingAsset covers the fail-fast branch. It is the
// one path that never runs in a correctly built binary, which is exactly why it
// went unmeasured — and why it is worth pinning: if it ever stopped panicking,
// a mis-built binary would emit an empty playbook at runtime instead of
// refusing to start, and the failure would surface as a silently useless
// prompt rather than a build error.
func TestMustReadStringPanicsOnMissingAsset(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("mustReadString returned for a missing asset instead of panicking")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("panic value = %T, want a string", r)
		}
		// The message must name the missing asset AND carry the underlying
		// error: "embedded file missing" alone does not say which one.
		for _, want := range []string{"assets: embedded file missing:", "prompts/does-not-exist.md"} {
			if !strings.Contains(msg, want) {
				t.Errorf("panic message %q missing %q", msg, want)
			}
		}
		if strings.HasSuffix(msg, "prompts/does-not-exist.md: ") {
			t.Errorf("panic message carries no underlying error: %q", msg)
		}
	}()
	_ = mustReadString("prompts/does-not-exist.md")
}

// TestMustReadStringReturnsRealAsset is the other arm: a present file comes
// back whole, so the guard is genuinely conditional rather than always-panicking.
func TestMustReadStringReturnsRealAsset(t *testing.T) {
	got := mustReadString("prompts/_interaction.md")
	if got == "" {
		t.Fatal("mustReadString returned empty for a file that is embedded")
	}
	if got != InteractionPlaybook {
		t.Error("mustReadString and the package-level playbook disagree on the same asset")
	}
}
