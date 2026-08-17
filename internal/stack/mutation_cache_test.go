package stack

import (
	"bytes"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

// TestGoProfileDeclaresMutationCache reads the EMBEDDED profile, not a fixture.
//
// A fixture would pass forever while the shipped go.toml quietly lost the
// declaration — and that loss is silent by construction: every Go repo would
// simply go back to compiling into the developer's shared cache, which is the
// state that reached 399 GB and filled a laptop.
func TestGoProfileDeclaresMutationCache(t *testing.T) {
	p, ok := builtinProfile(t, "go")
	if !ok {
		t.Fatal("no embedded go profile — this test is looking at the wrong thing")
	}
	if len(p.MutationCache.Vars) == 0 {
		t.Fatal("the shipped go profile declares no mutation_cache var; every Go repo silently reverts to the shared cache")
	}
	found := false
	for _, v := range p.MutationCache.Vars {
		if v == "GOCACHE" {
			found = true
		}
	}
	if !found {
		t.Errorf("go declares %v, want GOCACHE — the variable the Go toolchain actually reads", p.MutationCache.Vars)
	}
}

// TestProfileWithoutMutationCacheLoads: the block is additive. Every other
// shipped profile must load without it and must not gain the key on a re-encode
// — a profile with nothing to redirect is not opted out of anything, it simply
// has no cache to point somewhere else.
func TestProfileWithoutMutationCacheLoads(t *testing.T) {
	profiles := allBuiltins(t)
	if len(profiles) < 2 {
		t.Fatalf("expected several embedded profiles, got %d", len(profiles))
	}
	checked := 0
	for _, p := range profiles {
		if len(p.MutationCache.Vars) > 0 {
			continue
		}
		checked++
		if err := p.Validate(); err != nil {
			t.Errorf("profile %q without a mutation_cache does not validate: %v", p.ID, err)
		}
		var buf bytes.Buffer
		if err := toml.NewEncoder(&buf).Encode(p); err != nil {
			t.Fatalf("encode %q: %v", p.ID, err)
		}
		if strings.Contains(buf.String(), "mutation_cache") {
			t.Errorf("re-encoding %q invented a mutation_cache block:\n%s", p.ID, buf.String())
		}
	}
	if checked == 0 {
		t.Fatal("every embedded profile declares a cache var, so this test proved nothing")
	}
}

// TestMutationCacheRejectsBadVarName: these names reach an `export NAME=value`
// line in a script piped to a remote shell. A name carrying whitespace or an
// `=` would break the script or smuggle a second assignment into it, so it is
// refused at load rather than trusted to the quoting downstream.
func TestMutationCacheRejectsBadVarName(t *testing.T) {
	for _, tc := range []struct {
		name string
		v    string
		ok   bool
	}{
		{"ordinary", "GOCACHE", true},
		{"underscore lead", "_CACHE", true},
		{"digits after a letter", "CACHE2", true},
		{"empty", "", false},
		{"carries an equals", "GOCACHE=/tmp", false},
		{"carries a space", "GO CACHE", false},
		{"carries a newline", "GOCACHE\nexport EVIL", false},
		{"leading digit", "2CACHE", false},
		{"shell metacharacter", "GOCACHE;rm", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &Profile{ID: "x", MutationCache: MutationCache{Vars: []string{tc.v}}}
			err := p.Validate()
			if tc.ok && err != nil {
				t.Errorf("%q was rejected: %v", tc.v, err)
			}
			if !tc.ok && err == nil {
				t.Errorf("%q was accepted as an environment variable name", tc.v)
			}
			if !tc.ok && err != nil && !strings.Contains(err.Error(), "mutation_cache") {
				t.Errorf("the refusal does not name the field: %v", err)
			}
		})
	}
}

// TestMutationCacheRoundTrips: the list survives toml in order. Order matters
// because the assignments are emitted in it, and a reader diffing two runs'
// scripts should not see a reshuffle.
func TestMutationCacheRoundTrips(t *testing.T) {
	want := []string{"GOCACHE", "GOMODCACHE", "CARGO_TARGET_DIR"}
	in := &Profile{ID: "x", MutationCache: MutationCache{Vars: want}}

	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(in); err != nil {
		t.Fatal(err)
	}
	var out Profile
	if _, err := toml.Decode(buf.String(), &out); err != nil {
		t.Fatalf("decode: %v\n%s", err, buf.String())
	}
	if strings.Join(out.MutationCache.Vars, ",") != strings.Join(want, ",") {
		t.Errorf("round trip gave %v, want %v", out.MutationCache.Vars, want)
	}
}

// builtinProfile returns one embedded profile by id.
func builtinProfile(t *testing.T, id string) (*Profile, bool) {
	t.Helper()
	for _, p := range allBuiltins(t) {
		if p.ID == id {
			return p, true
		}
	}
	return nil, false
}

func allBuiltins(t *testing.T) []*Profile {
	t.Helper()
	ps, err := Embedded()
	if err != nil {
		t.Fatalf("load embedded profiles: %v", err)
	}
	return ps
}
