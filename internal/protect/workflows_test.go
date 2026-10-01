package protect

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// twoJobs is a jobs map every trigger form below shares: one job reporting
// its id, one reporting its name.
const twoJobs = `
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: go test ./...
  lint:
    name: Lint
    runs-on: ubuntu-latest
    steps:
      - run: make lint
`

var twoJobsWant = []Job{
	{Workflow: "ci.yml", ID: "test", Context: "test"},
	{Workflow: "ci.yml", ID: "lint", Context: "Lint"},
}

func scanOne(t *testing.T, src string) ([]Job, error) {
	t.Helper()
	return PullRequestJobs(map[string]string{"ci.yml": src})
}

// The trigger forms YAML allows for `on:` must agree. A scanner that only
// reads one of them silently drops a workflow's checks from the required set.
func TestTriggerFormParity(t *testing.T) {
	for _, tc := range []struct{ name, on string }{
		{"scalar", "on: pull_request\n"},
		{"scalar quoted", "on: \"pull_request\"\n"},
		{"flow list", "on: [push, pull_request]\n"},
		{"flow list quoted items", "on: [ 'push', \"pull_request\" ]\n"},
		{"flow map", "on: {push: {branches: [main]}, pull_request: {}}\n"},
		{"block map", "on:\n  push:\n    branches: [main]\n  pull_request:\n  workflow_dispatch:\n"},
		{"block map with types", "on:\n  pull_request:\n    types: [opened, synchronize, reopened]\n"},
		{"block map null value", "on:\n  pull_request: null\n"},
		{"block list", "on:\n  - push\n  - pull_request\n"},
		{"block list compact", "on:\n- push\n- pull_request\n"},
		{"double-quoted on", "\"on\": [push, pull_request]\n"},
		{"single-quoted on", "'on':\n  pull_request:\n"},
		{"comments around triggers", "on: # triggers\n  # pull_request_target:\n  pull_request: # every PR\n  push:\n"},
		{"crlf line endings", strings.ReplaceAll("on:\n  pull_request:\n  push:\n", "\n", "\r\n")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := tc.on + twoJobs
			if strings.Contains(tc.on, "\r\n") {
				src = tc.on + strings.ReplaceAll(twoJobs, "\n", "\r\n")
			}
			got, err := scanOne(t, src)
			if err != nil {
				t.Fatalf("unexpected refusal: %v", err)
			}
			if !reflect.DeepEqual(got, twoJobsWant) {
				t.Errorf("got %+v, want %+v", got, twoJobsWant)
			}
		})
	}
}

// pull_request_target runs the base branch's copy of a workflow, so its jobs
// never gate a PR's own commit. Prefix matching on "pull_request" would make
// t-5's Dependabot auto-merge job a required check that never reports.
func TestPullRequestTargetExcluded(t *testing.T) {
	for _, tc := range []struct{ name, src string }{
		{"flow list", "on: [push, pull_request_target]\n" + twoJobs},
		{"block map", "on:\n  pull_request_target:\n    types: [opened, synchronize]\n" + twoJobs},
		{"flow map", "on: {pull_request_target: {}}\n" + twoJobs},
		{"review events", "on: [pull_request_review, pull_request_review_comment]\n" + twoJobs},
		{"dependabot-automerge.yml shape", `name: dependabot-automerge

on: pull_request_target

permissions:
  contents: read

jobs:
  automerge:
    if: github.event.pull_request.user.login == 'dependabot[bot]'
    runs-on: ubuntu-latest
    permissions:
      contents: write
      pull-requests: write
    steps:
      - id: meta
        uses: dependabot/fetch-metadata@0000000000000000000000000000000000000000  # v2
      - if: steps.meta.outputs.update-type != 'version-update:semver-major'
        env:
          PR_URL: ${{ github.event.pull_request.html_url }}
          GH_TOKEN: ${{ github.token }}
        run: gh pr merge --auto --squash "$PR_URL"
`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := scanOne(t, tc.src)
			if err != nil || len(got) != 0 {
				t.Errorf("got %+v, %v; want 0 jobs and no error", got, err)
			}
		})
	}
}

// A workflow that never runs on pull_request contributes no required checks —
// release.yml's push-to-main shape included.
func TestOtherTriggersYieldNothing(t *testing.T) {
	for _, tc := range []struct{ name, src string }{
		{"push only", "on: push\n" + twoJobs},
		{"schedule", "on:\n  schedule:\n    - cron: '23 5 * * 1'\n  workflow_dispatch:\n" + twoJobs},
		{"release shape", "on:\n  push:\n    branches: [main]\n  workflow_dispatch:\n    inputs:\n      version:\n        description: |\n          pull_request: not a trigger\n" + twoJobs},
		{"no on key", twoJobs},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := scanOne(t, tc.src)
			if err != nil || len(got) != 0 {
				t.Errorf("got %+v, %v; want 0 jobs and no error", got, err)
			}
		})
	}
}

// A job reports its `name:` as its check context; only a job with no name
// reports its id.
func TestJobNameIsTheContext(t *testing.T) {
	for _, tc := range []struct{ name, line, want string }{
		{"plain name", "    name: Unit tests\n", "Unit tests"},
		{"double-quoted", "    name: \"Lint (go)\"\n", "Lint (go)"},
		{"single-quoted with escape", "    name: 'Bob''s tests'\n", "Bob's tests"},
		{"apostrophe then comment", "    name: Bob's tests  # the suite\n", "Bob's tests"},
		{"empty quoted name", "    name: \"\"\n", "unit"},
		{"no name", "", "unit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := "on: pull_request\njobs:\n  unit:\n    runs-on: ubuntu-latest\n" + tc.line + "    steps:\n      - run: go test ./...\n"
			got, err := scanOne(t, src)
			if err != nil {
				t.Fatalf("unexpected refusal: %v", err)
			}
			want := []Job{{Workflow: "ci.yml", ID: "unit", Context: tc.want}}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("got %+v, want %+v", got, want)
			}
		})
	}
}

// A job whose check name can't be known statically is refused, naming the
// workflow and job, never turned into a guessed context.
func TestRefusals(t *testing.T) {
	job := func(props string) string {
		return "on: pull_request\njobs:\n  build:\n    runs-on: ubuntu-latest\n" + props + "    steps:\n      - run: make\n"
	}
	for _, tc := range []struct{ name, src, job, reason string }{
		{"matrix block", job("    strategy:\n      fail-fast: false\n      matrix:\n        go: ['1.26', '1.27']\n"), "build", "matrix"},
		{"matrix flow", job("    strategy: {matrix: {go: ['1.26', '1.27']}}\n"), "build", "matrix"},
		{"matrix expression", job("    strategy:\n      matrix: ${{ fromJSON(needs.plan.outputs.m) }}\n"), "build", "matrix"},
		{"job-level uses", "on: pull_request\njobs:\n  build:\n    uses: org/repo/.github/workflows/build.yml@0123456789abcdef0123456789abcdef01234567\n", "build", "reusable workflow"},
		{"expression name", job("    name: build ${{ matrix.os }}\n"), "build", "expression"},
		{"block scalar name", job("    name: >-\n      build\n"), "build", "block scalar"},
		{"anchor merge", job("    <<: *defaults\n"), "build", "anchor"},
		{"inline job body", "on: pull_request\njobs:\n  build: {runs-on: ubuntu-latest}\n", "build", "block mapping"},
		{"paths filter", "on:\n  pull_request:\n    paths: ['**.go']\n" + twoJobs, "test,lint", "`paths`"},
		{"paths-ignore filter", "on:\n  pull_request:\n    types: [opened]\n    paths-ignore:\n      - docs/**\n" + twoJobs, "test,lint", "`paths-ignore`"},
		{"branches filter", "on:\n  pull_request:\n    branches: [main]\n" + twoJobs, "test,lint", "`branches`"},
		{"branches-ignore filter", "on:\n  pull_request:\n    branches-ignore: [wip/**]\n" + twoJobs, "test,lint", "`branches-ignore`"},
		{"inline flow filter", "on:\n  pull_request: {branches: [main]}\n" + twoJobs, "test,lint", "`branches`"},
		{"flow map filter", "on: {push: {}, pull_request: {paths: ['**.go']}}\n" + twoJobs, "test,lint", "`paths`"},
		{"multi-line flow on", "on: [push,\n  pull_request]\n" + twoJobs, "test,lint", "spans lines"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := scanOne(t, tc.src)
			var r *Refusal
			if !errors.As(err, &r) {
				t.Fatalf("got %+v, %v; want a *Refusal", got, err)
			}
			if got != nil {
				t.Errorf("a refusal must return no jobs, got %+v", got)
			}
			if r.Workflow != "ci.yml" || r.Job != tc.job {
				t.Errorf("refusal names %s/%s, want ci.yml/%s", r.Workflow, r.Job, tc.job)
			}
			if !strings.Contains(r.Reason, tc.reason) {
				t.Errorf("reason %q does not mention %q", r.Reason, tc.reason)
			}
		})
	}
}

// Nothing but a key at the jobs map's own indent is a job: not a run: script
// that writes a workflow, not a comment, not a step's name:.
func TestFalseJobs(t *testing.T) {
	want := []Job{{Workflow: "ci.yml", ID: "test", Context: "test"}}
	for _, tc := range []struct{ name, src string }{
		{"run body embeds a workflow with fake: at job-key indent", `on: pull_request
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - name: write a fixture workflow
        run: |
          cat > fixture.yml <<'EOF'
          jobs:
            fake:
              name: Fake
              uses: org/repo/.github/workflows/x.yml@v1
              strategy:
                matrix:
          EOF
`},
		{"commented-out jobs", `on: pull_request
# jobs:
#   fake:
jobs:
  # fake:
  #   runs-on: ubuntu-latest
  test:
    runs-on: ubuntu-latest
    # name: Fake
    steps:
      - run: go test ./...
`},
		{"step names and step uses", `on: pull_request
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - name: Unit tests
        uses: actions/checkout@0000000000000000000000000000000000000000
      - name: Fake
        run: go test ./...
`},
		{"compact step names and step uses", `on: pull_request
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
    - name: Unit tests
      uses: actions/checkout@0000000000000000000000000000000000000000
    - name: Fake
      run: go test ./...
`},
		{"block-scalar if holding key-shaped text", `on: pull_request
jobs:
  test:
    if: |
      github.event_name == 'pull_request'
    name: test
    runs-on: ubuntu-latest
    steps:
      - run: |
          name: Fake
          strategy:
`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := scanOne(t, tc.src)
			if err != nil {
				t.Fatalf("unexpected refusal: %v", err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("got %+v, want %+v", got, want)
			}
		})
	}
}

// One unreadable job refuses the whole set: a partial list would read as a
// smaller required set rather than an unknown one.
func TestRefusalReturnsNoJobs(t *testing.T) {
	got, err := PullRequestJobs(map[string]string{
		"a.yml": "on: pull_request\n" + twoJobs,
		"b.yml": "on: pull_request\njobs:\n  build:\n    strategy:\n      matrix:\n        os: [a, b]\n",
	})
	var r *Refusal
	if !errors.As(err, &r) || r.Workflow != "b.yml" || r.Job != "build" {
		t.Fatalf("got %+v, %v; want b.yml/build refused", got, err)
	}
	if got != nil {
		t.Errorf("want no jobs alongside a refusal, got %+v", got)
	}
}

func TestJobsOrderedByPathThenPosition(t *testing.T) {
	got, err := PullRequestJobs(map[string]string{
		"b.yml": "on: pull_request\njobs:\n  zeta:\n    runs-on: x\n  alpha:\n    runs-on: x\n",
		"a.yml": "on: pull_request\njobs:\n  only:\n    runs-on: x\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []Job{
		{Workflow: "a.yml", ID: "only", Context: "only"},
		{Workflow: "b.yml", ID: "zeta", Context: "zeta"},
		{Workflow: "b.yml", ID: "alpha", Context: "alpha"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestContextsSortedAndDistinct(t *testing.T) {
	got := Contexts([]Job{
		{Workflow: "b.yml", Context: "test"},
		{Workflow: "a.yml", Context: "Lint"},
		{Workflow: "a.yml", Context: "test"},
	})
	if want := []string{"Lint", "test"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if got := Contexts(nil); len(got) != 0 {
		t.Errorf("no jobs must give no contexts, got %v", got)
	}
}

// The error text is the `<workflow>/<job>: <reason>` doctor renders inside
// `unknown (…)`.
func TestRefusalErrorNamesWorkflowAndJob(t *testing.T) {
	r := &Refusal{Workflow: ".github/workflows/ci.yml", Job: "build", Reason: "it runs a matrix"}
	if got, want := r.Error(), ".github/workflows/ci.yml/build: it runs a matrix"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
