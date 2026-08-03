package ship

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
)

// TestOpenPRsTargetingGitHub drives the GitHub path with a stubbed ghCommand
// whose stdout is a canned `gh pr list --json` document, and captures the args
// so a regression that drops --base, --state open or a json field is caught.
func TestOpenPRsTargetingGitHub(t *testing.T) {
	var gotArgs []string
	prev := ghCommand
	ghCommand = func(args ...string) *exec.Cmd {
		gotArgs = append([]string{}, args...)
		return exec.Command("printf", "%s", `[{"number":7,"title":"v1.3 integration","url":"https://example.test/pr/7","headRefName":"milestone/v1.3"}]`)
	}
	defer func() { ghCommand = prev }()

	prs, err := OpenPRsTargeting(OpenOpts{Provider: "github"}, "milestone/v1.2")
	if err != nil {
		t.Fatalf("OpenPRsTargeting: %v", err)
	}
	if len(prs) != 1 {
		t.Fatalf("got %d PRs, want 1: %+v", len(prs), prs)
	}
	if prs[0].Number != 7 {
		t.Errorf("number = %d, want 7", prs[0].Number)
	}
	if prs[0].URL != "https://example.test/pr/7" {
		t.Errorf("url = %q", prs[0].URL)
	}
	if prs[0].HeadRefName != "milestone/v1.3" {
		t.Errorf("headRefName = %q", prs[0].HeadRefName)
	}
	if prs[0].Title != "v1.3 integration" {
		t.Errorf("title = %q", prs[0].Title)
	}

	want := []string{"pr", "list", "--base", "milestone/v1.2", "--state", "open", "--json", "number,title,url,headRefName"}
	if len(gotArgs) != len(want) {
		t.Fatalf("gh args: got %v want %v", gotArgs, want)
	}
	for i := range want {
		if gotArgs[i] != want[i] {
			t.Errorf("gh arg %d: got %q want %q", i, gotArgs[i], want[i])
		}
	}
}

func TestOpenPRsTargetingGitHubNoResults(t *testing.T) {
	prev := ghCommand
	ghCommand = func(...string) *exec.Cmd { return exec.Command("printf", "%s", `[]`) }
	defer func() { ghCommand = prev }()

	prs, err := OpenPRsTargeting(OpenOpts{Provider: "github"}, "milestone/v1.2")
	if err != nil {
		t.Fatalf("OpenPRsTargeting: %v", err)
	}
	if len(prs) != 0 {
		t.Errorf("got %+v, want no PRs", prs)
	}
}

// A failed lookup must be an error, never an empty slice: an empty slice reads
// as "no dependents" and authorizes an irreversible branch delete.
func TestOpenPRsTargetingGitHubFailureIsAnError(t *testing.T) {
	prev := ghCommand
	ghCommand = func(...string) *exec.Cmd { return exec.Command("sh", "-c", "echo boom >&2; exit 1") }
	defer func() { ghCommand = prev }()

	prs, err := OpenPRsTargeting(OpenOpts{Provider: "github"}, "milestone/v1.2")
	if err == nil {
		t.Fatalf("a non-zero gh exit returned %+v with no error", prs)
	}
	if prs != nil {
		t.Errorf("expected no PRs alongside the error, got %+v", prs)
	}
}

func TestOpenPRsTargetingGitHubMalformedJSON(t *testing.T) {
	prev := ghCommand
	ghCommand = func(...string) *exec.Cmd { return exec.Command("printf", "%s", `[{"number":`) }
	defer func() { ghCommand = prev }()

	prs, err := OpenPRsTargeting(OpenOpts{Provider: "github"}, "milestone/v1.2")
	if err == nil {
		t.Fatalf("malformed JSON parsed into %+v with no error", prs)
	}
	if !strings.Contains(err.Error(), "parse") {
		t.Errorf("error should name the parse failure: %v", err)
	}
}

func TestOpenPRsTargetingGitHubEmptyBase(t *testing.T) {
	prev := ghCommand
	ghCommand = func(...string) *exec.Cmd {
		t.Error("an empty base must be rejected before shelling out")
		return exec.Command("printf", "%s", `[]`)
	}
	defer func() { ghCommand = prev }()

	if _, err := OpenPRsTargeting(OpenOpts{Provider: "github"}, ""); err == nil {
		t.Error("expected an error for an empty base branch")
	}
}

// The unwired providers return the sentinel without shelling out, so the caller
// announces a skip instead of mistaking "cannot answer" for "no dependents".
func TestOpenPRsTargetingUnsupportedProvider(t *testing.T) {
	prev := ghCommand
	ghCommand = func(...string) *exec.Cmd {
		t.Error("an unsupported provider must not shell out to gh")
		return exec.Command("printf", "%s", `[]`)
	}
	defer func() { ghCommand = prev }()

	for _, prov := range []string{"bitbucket", "forgejo", "gitea", "Forgejo"} {
		prs, err := OpenPRsTargeting(OpenOpts{Provider: prov}, "milestone/v1.2")
		if !errors.Is(err, ErrBasePRLookupUnsupported) {
			t.Errorf("provider %q: err=%v want ErrBasePRLookupUnsupported", prov, err)
		}
		if prs != nil {
			t.Errorf("provider %q: expected no PRs, got %+v", prov, prs)
		}
	}
}

// An outright unknown provider is a misconfiguration, distinguishable from a
// backend that simply is not wired yet.
func TestOpenPRsTargetingUnknownProvider(t *testing.T) {
	_, err := OpenPRsTargeting(OpenOpts{Provider: "perforce"}, "milestone/v1.2")
	if err == nil {
		t.Fatal("expected an error for an unknown provider")
	}
	if errors.Is(err, ErrBasePRLookupUnsupported) {
		t.Error("an unknown provider should not report as merely unsupported")
	}
}

func TestOpenPRsTargetingFuncDefaultsToOpenPRsTargeting(t *testing.T) {
	if OpenPRsTargetingFunc == nil {
		t.Fatal("OpenPRsTargetingFunc must be a non-nil overridable var")
	}
	if _, err := OpenPRsTargetingFunc(OpenOpts{Provider: "forgejo"}, "milestone/v1.2"); !errors.Is(err, ErrBasePRLookupUnsupported) {
		t.Errorf("OpenPRsTargetingFunc should delegate to OpenPRsTargeting, got: %v", err)
	}
}
