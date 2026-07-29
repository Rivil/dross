package configenum

import (
	"reflect"
	"testing"
)

func TestSetNormalizeTrimsAndLowercases(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"version", "version"},
		{"Version", "version"},
		{" version", "version"},
		{"  VERSION\t", "version"},
		{"\n Private-Token ", "private-token"},
		{"", ""},
		{"   ", ""},
	}
	for _, c := range cases {
		if got := Normalize(c.in); got != c.want {
			t.Errorf("Normalize(%q) = %q, want %q", c.in, got, c.want)
		}
	}

	// The point of a shared Normalize is that Has is exactly as forgiving as
	// dispatch — a value doctor blesses must be one a consumer switch matches.
	for _, v := range []string{"Jira", " jira", "JIRA\t"} {
		if !BoardProviders.Has(v) {
			t.Errorf("BoardProviders.Has(%q) = false, want true", v)
		}
	}
	if BoardProviders.Has("ji ra") {
		t.Error("BoardProviders.Has(\"ji ra\") = true; Normalize trims edges, it does not strip inner space")
	}
}

func TestEmptyValuePoliciesDiffer(t *testing.T) {
	// Empty auth_scheme defaults to private-token in code -> accepted.
	if !AuthSchemes.Has("") {
		t.Error("AuthSchemes.Has(\"\") = false, want true (empty defaults to private-token)")
	}
	// Empty milestone_mode defaults to version in code -> accepted.
	if !MilestoneModes.Has("") {
		t.Error("MilestoneModes.Has(\"\") = false, want true (empty defaults to version)")
	}
	// Empty [board].provider dispatches nowhere -> still invalid.
	if BoardProviders.Has("") {
		t.Error("BoardProviders.Has(\"\") = true, want false (an unset provider has no backend)")
	}
	if BoardProviders.Has("   ") {
		t.Error("BoardProviders.Has(\"   \") = true; whitespace normalises to empty and must stay invalid")
	}
	// Same policy for ship: "none" is a call-site sentinel, not a member.
	if ShipProviders.Has("") {
		t.Error("ShipProviders.Has(\"\") = true, want false")
	}
	if ShipProviders.Has("none") {
		t.Error("ShipProviders.Has(\"none\") = true; none means \"no remote\" and is not a dispatchable backend")
	}
}

func TestForgeRESTProvidersIsSubsetOfBoardProviders(t *testing.T) {
	for _, v := range ForgeRESTProviders.Values() {
		if !BoardProviders.Has(v) {
			t.Errorf("ForgeRESTProviders has %q, which BoardProviders rejects", v)
		}
	}
	want := []string{"forgejo", "gitea", "gitlab"}
	if got := ForgeRESTProviders.Values(); !reflect.DeepEqual(got, want) {
		t.Errorf("ForgeRESTProviders.Values() = %v, want %v", got, want)
	}
	// It is a strict subset: youtrack/jira/github have bespoke clients.
	for _, v := range []string{"youtrack", "jira", "github"} {
		if ForgeRESTProviders.Has(v) {
			t.Errorf("ForgeRESTProviders.Has(%q) = true; it is not served by the generic REST client", v)
		}
	}
}

func TestAuthSchemesIncludeBasic(t *testing.T) {
	for _, v := range []string{"private-token", "bearer", "basic"} {
		if !AuthSchemes.Has(v) {
			t.Errorf("AuthSchemes.Has(%q) = false, want true", v)
		}
	}
	if AuthSchemes.Has("digest") {
		t.Error("AuthSchemes.Has(\"digest\") = true, want false")
	}
}

func TestShipProvidersIncludeBitbucket(t *testing.T) {
	for _, v := range []string{"github", "forgejo", "gitea", "gitlab", "bitbucket"} {
		if !ShipProviders.Has(v) {
			t.Errorf("ShipProviders.Has(%q) = false, want true", v)
		}
	}
	if ShipProviders.Has("sourcehut") {
		t.Error("ShipProviders.Has(\"sourcehut\") = true, want false")
	}
	// bitbucket is a remote, never a board backend.
	if BoardProviders.Has("bitbucket") {
		t.Error("BoardProviders.Has(\"bitbucket\") = true; bitbucket has no board backend")
	}
}

func TestMilestoneModesForProvider(t *testing.T) {
	yt := MilestoneModesFor("youtrack")
	if yt == nil {
		t.Fatal("MilestoneModesFor(\"youtrack\") = nil, want the full mode set")
	}
	if got, want := yt.Values(), []string{"version", "agile", "epic"}; !reflect.DeepEqual(got, want) {
		t.Errorf("youtrack modes = %v, want %v", got, want)
	}

	jira := MilestoneModesFor("jira")
	if jira == nil {
		t.Fatal("MilestoneModesFor(\"jira\") = nil, want [version]")
	}
	if got, want := jira.Values(), []string{"version"}; !reflect.DeepEqual(got, want) {
		t.Errorf("jira modes = %v, want %v — jira.go errors on anything but version", got, want)
	}
	if jira.Has("epic") {
		t.Error("jira accepts epic; that combination fails at milestone-sync time")
	}
	// Empty still means version, even on the narrowed jira set.
	if !jira.Has("") {
		t.Error("jira.Has(\"\") = false, want true (empty defaults to version)")
	}

	// milestone_mode does not apply to the REST forges or github.
	for _, p := range []string{"gitlab", "forgejo", "gitea", "github", "", "sourcehut"} {
		if got := MilestoneModesFor(p); got != nil {
			t.Errorf("MilestoneModesFor(%q) = %v, want nil", p, got.Values())
		}
	}

	// Provider lookup normalises like every other read.
	if MilestoneModesFor(" YouTrack ") == nil {
		t.Error("MilestoneModesFor(\" YouTrack \") = nil; provider lookup must Normalize")
	}
}

func TestBoardRequiresBaseURL(t *testing.T) {
	if BoardRequiresBaseURL("github") {
		t.Error("BoardRequiresBaseURL(\"github\") = true; the github backend defaults to https://api.github.com")
	}
	if BoardRequiresBaseURL(" GitHub ") {
		t.Error("BoardRequiresBaseURL(\" GitHub \") = true; the lookup must Normalize")
	}
	for _, p := range []string{"forgejo", "gitea", "gitlab", "youtrack", "jira"} {
		if !BoardRequiresBaseURL(p) {
			t.Errorf("BoardRequiresBaseURL(%q) = false; only github has an address to guess", p)
		}
	}
}

func TestSetListRendersPipeJoined(t *testing.T) {
	cases := []struct {
		name string
		set  Set
		want string
	}{
		{"board", BoardProviders, "forgejo | gitea | gitlab | youtrack | jira | github"},
		{"forgeREST", ForgeRESTProviders, "forgejo | gitea | gitlab"},
		{"ship", ShipProviders, "github | forgejo | gitea | gitlab | bitbucket"},
		{"auth", AuthSchemes, "private-token | bearer | basic"},
		{"milestone", MilestoneModes, "version | agile | epic"},
	}
	for _, c := range cases {
		if got := c.set.List(); got != c.want {
			t.Errorf("%s List() = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestValuesIsDefensiveCopy(t *testing.T) {
	got := ShipProviders.Values()
	got[0] = "clobbered"
	if ShipProviders.Values()[0] != "github" {
		t.Error("Values() aliases the package-level Set; a caller mutated it")
	}
}

// TestMilestoneStatusesMatchesDisk pins the set to what the milestone tomls
// actually carry (D1), not to the doc comment that once said
// shipped|archived. `milestone complete` writes no status at all, so the only
// writers are `milestone create` ("planning") and the generic setter.
func TestMilestoneStatusesMatchesDisk(t *testing.T) {
	if got, want := MilestoneStatuses.Values(), []string{"planning", "active", "complete"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("MilestoneStatuses = %v, want %v", got, want)
	}
	for _, v := range []string{"planning", "active", "complete", "Complete", " active "} {
		if !MilestoneStatuses.Has(v) {
			t.Errorf("Has(%q) = false, want true", v)
		}
	}
	for _, v := range []string{"shipped", "archived", "", "   ", "done"} {
		if MilestoneStatuses.Has(v) {
			t.Errorf("Has(%q) = true, want false", v)
		}
	}
}
