package defaults

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Rivil/dross/internal/project"
)

func TestLoadMissingReturnsEmpty(t *testing.T) {
	d, err := LoadFile(filepath.Join(t.TempDir(), "nope.toml"))
	if err != nil {
		t.Fatalf("missing file should be ok: %v", err)
	}
	if !reflect.DeepEqual(d.Remote, RemoteDefaults{}) {
		t.Errorf("expected zero RemoteDefaults, got %+v", d.Remote)
	}
}

func TestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, File)
	original := &Defaults{
		Remote: RemoteDefaults{
			Provider:  "forgejo",
			APIBase:   "https://forge.example/api/v1",
			LogAPI:    true,
			AuthEnv:   "FORGEJO_TOKEN",
			Reviewers: []string{"alice"},
		},
	}
	if err := original.SaveFile(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded, err := LoadFile(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !reflect.DeepEqual(original, loaded) {
		t.Fatalf("round-trip mismatch:\norig:   %+v\nloaded: %+v", original, loaded)
	}
}

func TestSaveFileCreatesParentDir(t *testing.T) {
	dir := t.TempDir()
	deep := filepath.Join(dir, "a", "b", "c", File)
	d := &Defaults{Remote: RemoteDefaults{Provider: "github"}}
	if err := d.SaveFile(deep); err != nil {
		t.Fatalf("save deep: %v", err)
	}
}

func TestApplyOnlyFillsZeroFields(t *testing.T) {
	d := Defaults{Remote: RemoteDefaults{
		Provider:  "forgejo",
		APIBase:   "https://forge/api/v1",
		LogAPI:    true,
		AuthEnv:   "FORGEJO_TOKEN",
		Reviewers: []string{"alice", "bob"},
	}}

	t.Run("seeds zero remote", func(t *testing.T) {
		got := d.Apply(project.Remote{URL: "https://forge/me/p"})
		if got.Provider != "forgejo" {
			t.Errorf("Provider should be filled: %q", got.Provider)
		}
		if got.APIBase != "https://forge/api/v1" {
			t.Errorf("APIBase: %q", got.APIBase)
		}
		if !got.LogAPI {
			t.Error("LogAPI should be filled true")
		}
		if got.AuthEnv != "FORGEJO_TOKEN" {
			t.Errorf("AuthEnv: %q", got.AuthEnv)
		}
		if !reflect.DeepEqual(got.Reviewers, []string{"alice", "bob"}) {
			t.Errorf("Reviewers: %+v", got.Reviewers)
		}
		// URL must be preserved.
		if got.URL != "https://forge/me/p" {
			t.Errorf("URL clobbered: %q", got.URL)
		}
	})

	t.Run("does not overwrite already-set fields", func(t *testing.T) {
		got := d.Apply(project.Remote{
			URL:       "https://gh/me/p",
			Provider:  "github",
			APIBase:   "https://api.github.com",
			LogAPI:    false,
			AuthEnv:   "GITHUB_TOKEN",
			Reviewers: []string{"carol"},
		})
		if got.Provider != "github" {
			t.Errorf("Provider was overwritten: %q", got.Provider)
		}
		if got.APIBase != "https://api.github.com" {
			t.Errorf("APIBase was overwritten: %q", got.APIBase)
		}
		// LogAPI default=true should NOT promote a false-by-design field —
		// but we can't distinguish unset from explicit-false on bool, so
		// the rule is "if remote already false and default true, take true".
		// Document by way of test: the override happens.
		if !got.LogAPI {
			t.Error("LogAPI: when remote=false and default=true, default should fill (bool zero is indistinguishable from unset)")
		}
		if got.AuthEnv != "GITHUB_TOKEN" {
			t.Errorf("AuthEnv was overwritten: %q", got.AuthEnv)
		}
		if !reflect.DeepEqual(got.Reviewers, []string{"carol"}) {
			t.Errorf("Reviewers: %+v", got.Reviewers)
		}
	})
}

func TestFromRemote(t *testing.T) {
	r := project.Remote{
		URL:       "https://forge/me/p",
		Provider:  "forgejo",
		Public:    false,
		APIBase:   "https://forge/api/v1",
		LogAPI:    true,
		AuthEnv:   "FORGEJO_TOKEN",
		Reviewers: []string{"alice"},
	}
	got := FromRemote(r)
	want := RemoteDefaults{
		Provider:  "forgejo",
		APIBase:   "https://forge/api/v1",
		LogAPI:    true,
		AuthEnv:   "FORGEJO_TOKEN",
		Reviewers: []string{"alice"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("FromRemote:\ngot:  %+v\nwant: %+v", got, want)
	}
}
