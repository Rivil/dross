package consent

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/BurntSushi/toml"
)

// fileStore is the test Store: a TOML file holding exactly a Grants. Absent
// reads as empty, like the production store.
type fileStore struct{ path string }

func (s fileStore) Load() (*Grants, error) {
	var g Grants
	_, err := toml.DecodeFile(s.path, &g)
	if errors.Is(err, fs.ErrNotExist) {
		return &Grants{}, nil
	}
	if err != nil {
		return nil, err
	}
	return &g, nil
}

func (s fileStore) Save(g *Grants) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(s.path)
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(g)
}

// consentFixture builds a git work tree with a .dross root and returns the
// store over its local.toml plus (root, repoDir).
func consentFixture(t *testing.T) (fileStore, string, string) {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-q")
	git(t, dir, "config", "user.email", "t@example.com")
	git(t, dir, "config", "user.name", "t")
	root := filepath.Join(dir, ".dross")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	return fileStore{path: filepath.Join(root, File)}, root, dir
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// TestRefuseTrackedLocalOnlyOnTracked: a missing or untracked store is fine;
// only "git says this is tracked" refuses, naming the remedy.
func TestRefuseTrackedLocalOnlyOnTracked(t *testing.T) {
	store, _, repoDir := consentFixture(t)
	if err := RefuseTrackedLocal(repoDir); err != nil {
		t.Fatalf("a missing local.toml was refused: %v", err)
	}
	if err := store.Save(&Grants{TrustedTestCommand: Fingerprint("x")}); err != nil {
		t.Fatal(err)
	}
	if err := RefuseTrackedLocal(repoDir); err != nil {
		t.Fatalf("an untracked local.toml was refused: %v", err)
	}
	git(t, repoDir, "add", "-f", RelPath)
	err := RefuseTrackedLocal(repoDir)
	if err == nil {
		t.Fatal("a tracked local.toml was not refused")
	}
	for _, want := range []string{"refusing to read", RelPath, "tracked", "git rm --cached"} {
		if !contains(err.Error(), want) {
			t.Errorf("refusal does not mention %q: %v", want, err)
		}
	}
	git(t, repoDir, "rm", "--cached", "-q", RelPath)
	if err := RefuseTrackedLocal(repoDir); err != nil {
		t.Fatalf("after untracking, still refused: %v", err)
	}
}

// TestRelPathNamesTheStore: the path git is asked about is the store's own.
func TestRelPathNamesTheStore(t *testing.T) {
	if RelPath != ".dross/"+File || File != "local.toml" {
		t.Errorf("RelPath = %q, File = %q", RelPath, File)
	}
}
