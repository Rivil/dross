package defaults

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
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

// TestTelemetryEnabledUnsetIsOn pins the "unset = on" default and both explicit
// settings. The nil check is the whole opt-out contract: inverted, a user who
// never answered the prompt would be treated as having opted OUT, and dross
// would silently stop recording — indistinguishable, from the outside, from
// telemetry working.
func TestTelemetryEnabledUnsetIsOn(t *testing.T) {
	on, off := true, false
	cases := []struct {
		name string
		in   *bool
		want bool
	}{
		{"unset means on", nil, true},
		{"explicitly on", &on, true},
		{"explicitly off", &off, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := TelemetryDefaults{Enabled: tc.in}.TelemetryEnabled()
			if got != tc.want {
				t.Errorf("TelemetryEnabled() = %v, want %v", got, tc.want)
			}
		})
	}

	// The explicit-off case is what makes the nil check load-bearing: if the
	// guard were dropped entirely, unset would dereference nil; if it were
	// inverted, unset and explicit-off would agree. Neither can pass the table.
	if (TelemetryDefaults{Enabled: &off}).TelemetryEnabled() == (TelemetryDefaults{}).TelemetryEnabled() {
		t.Error("unset and explicitly-off must not agree — unset is on")
	}
}

// writeDefaults writes body as a defaults.toml in a temp dir and returns its path.
func writeDefaults(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), File)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestContextThresholdUnsetVsZero pins the pointer: unset is the 150k default
// and an explicit 0 is "off". A non-pointer field could not tell them apart,
// so `threshold = 0` would silently read as the default.
func TestContextThresholdUnsetVsZero(t *testing.T) {
	unset, err := LoadFile(writeDefaults(t, "[telemetry]\n  enabled = true\n"))
	if err != nil {
		t.Fatalf("load unset: %v", err)
	}
	if got, err := unset.Context.EffectiveThreshold(); err != nil || got != 150_000 {
		t.Errorf("no [context]: EffectiveThreshold = %d, %v; want 150000, nil", got, err)
	}

	zero, err := LoadFile(writeDefaults(t, "[context]\n  threshold = 0\n"))
	if err != nil {
		t.Fatalf("load zero: %v", err)
	}
	if got, err := zero.Context.EffectiveThreshold(); err != nil || got != 0 {
		t.Errorf("threshold = 0: EffectiveThreshold = %d, %v; want 0, nil", got, err)
	}

	set, err := LoadFile(writeDefaults(t, "[context]\n  threshold = 90000\n"))
	if err != nil {
		t.Fatalf("load set: %v", err)
	}
	if got, err := set.Context.EffectiveThreshold(); err != nil || got != 90_000 {
		t.Errorf("threshold = 90000: EffectiveThreshold = %d, %v; want 90000, nil", got, err)
	}
}

// TestContextThresholdSurvivesOtherWriters: `defaults save` rewrites Remote and
// `stats opt-out` rewrites Telemetry, each by load → mutate → SaveFile. Neither
// may drop an explicit `threshold = 0`, and a file with no [context] must not
// gain one.
func TestContextThresholdSurvivesOtherWriters(t *testing.T) {
	path := writeDefaults(t, "[telemetry]\n  enabled = false\n\n[context]\n  threshold = 0\n")
	d, err := LoadFile(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	d.Remote = RemoteDefaults{Provider: "github", AuthEnv: "GITHUB_TOKEN"}
	on := true
	d.Telemetry.Enabled = &on
	d.Telemetry.AskedAt = "2026-10-08"
	if err := d.SaveFile(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	reloaded, err := LoadFile(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.Context.Threshold == nil {
		t.Fatal("threshold = 0 was dropped by a save that only touched remote_defaults and telemetry")
	}
	if got, err := reloaded.Context.EffectiveThreshold(); err != nil || got != 0 {
		t.Errorf("after re-save: EffectiveThreshold = %d, %v; want 0, nil", got, err)
	}

	bare := writeDefaults(t, "[remote_defaults]\n  provider = \"gitlab\"\n")
	b, err := LoadFile(bare)
	if err != nil {
		t.Fatalf("load bare: %v", err)
	}
	if err := b.SaveFile(bare); err != nil {
		t.Fatalf("save bare: %v", err)
	}
	raw, err := os.ReadFile(bare)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "[context]") {
		t.Errorf("a file with no [context] re-saved with one:\n%s", raw)
	}
}

// TestContextThresholdValidation: a negative value errors only where the
// threshold is read, so every other defaults reader keeps working; a value of
// the wrong type fails the load naming the file, never a silent default.
func TestContextThresholdValidation(t *testing.T) {
	neg, err := LoadFile(writeDefaults(t, "[context]\n  threshold = -1\n"))
	if err != nil {
		t.Fatalf("threshold = -1 must still load (other readers keep working): %v", err)
	}
	if got, err := neg.Context.EffectiveThreshold(); err == nil {
		t.Errorf("threshold = -1: EffectiveThreshold = %d, nil; want an error", got)
	}

	path := writeDefaults(t, "[context]\n  threshold = \"150k\"\n")
	d, err := LoadFile(path)
	if err == nil {
		t.Fatalf("threshold = \"150k\" loaded as %+v; want a decode error", d.Context)
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("decode error %q does not name %s", err, path)
	}
}

// TestContextDefaultsOneKnob: the threshold is the only setting. The 50k step
// between nudges is fixed in code (locked decision nudge_cadence), so a second
// field here would be a second knob.
func TestContextDefaultsOneKnob(t *testing.T) {
	typ := reflect.TypeOf(ContextDefaults{})
	if typ.NumField() != 1 || typ.Field(0).Name != "Threshold" {
		var names []string
		for i := 0; i < typ.NumField(); i++ {
			names = append(names, typ.Field(i).Name)
		}
		t.Errorf("ContextDefaults fields = %v; want exactly [Threshold]", names)
	}
}
