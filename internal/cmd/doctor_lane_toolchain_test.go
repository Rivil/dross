package cmd

// `dross doctor`'s Remote section, reporting each declared lane's toolchain
// against the granted host (c-8).
//
// The reason this belongs in doctor at all is timing: a locality fallback is
// otherwise first seen halfway through a run, in a transcript nobody reads
// until something goes wrong. The reason it belongs in the SAME probe is
// agreement — a second question is a second answer, and doctor passing on a
// host the run then falls back from is worse than no section at all.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/project"
	"github.com/Rivil/dross/internal/remote"
	"github.com/Rivil/dross/internal/testlane"
)

// doctorLaneFixture is doctorRemoteFixture with lanes appended.
//
// It also pins the PATH lookup, marking every adapter tool present on THIS
// machine. These tests are about the Remote section, but checkMutationToolchain
// walks the real PATH for the same adapters and prints its own advisory — so
// without the pin, whether a developer happens to have gremlins installed
// decides what doctor's output contains, and an assertion over that output
// passes here and fails in CI (it did: TestDoctorLaneGapIsNotAnIssue, run
// 33247017255). Present rather than absent because the local advisory is not
// what any test in this file is asserting; silencing it leaves only the remote
// section these tests exist to check.
func doctorLaneFixture(t *testing.T, host string, adapters []string, lanes string) {
	t.Helper()
	doctorRemoteFixture(t, host, "/srv/dross", adapters)
	present := map[string]bool{}
	for _, tool := range remoteAdapterTools {
		present[tool] = true
	}
	fakeLookPath(t, present)
	if lanes == "" {
		return
	}
	root, err := FindRoot()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, project.File)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(string(b)+"\n"+lanes+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

const doctorGoAndWebLanes = `[[runtime.test_lane]]
name = "go"
match = ["internal/**"]
command = "go test -count=1 ./..."

[[runtime.test_lane]]
name = "web"
match = ["web/**"]
command = "pnpm test"`

// TestDoctorNamesTheLaneMissingItsToolchain is c-8's report. Both rows are
// asserted: naming only the gap would leave the reader unable to tell a lane
// doctor checked and cleared from one it never looked at.
func TestDoctorNamesTheLaneMissingItsToolchain(t *testing.T) {
	doctorLaneFixture(t, "helicon", nil, doctorGoAndWebLanes)
	fakeProbe(t, func(_ remote.Target, _ []string) (remote.Readiness, error) {
		return remote.Readiness{Cores: 8, Missing: []string{"pnpm"}}, nil
	})

	var out string
	doctorIssues(t, &out)

	gap := lineContaining(t, out, "lane web will run")
	for _, want := range []string{"web", "pnpm", "helicon"} {
		if !strings.Contains(gap, want) {
			t.Errorf("the lane row does not name %q: %q", want, gap)
		}
	}
	ok := lineContaining(t, out, "lane go toolchain")
	if !strings.Contains(ok, "go") || !strings.Contains(ok, "helicon") {
		t.Errorf("the cleared lane's row does not report its toolchain against the host: %q", ok)
	}
	if strings.Contains(ok, "has no") {
		t.Errorf("a lane whose toolchain is present was reported as a gap: %q", ok)
	}
}

// TestDoctorLaneGapIsNotAnIssue: a lane that falls back still runs and still
// reports its own suite result, so failing doctor on it would fail a repo that
// works. An adapter's missing tool has no local fallback and still counts.
func TestDoctorLaneGapIsNotAnIssue(t *testing.T) {
	doctorLaneFixture(t, "helicon", []string{"gremlins"}, doctorGoAndWebLanes)

	fakeProbe(t, func(_ remote.Target, _ []string) (remote.Readiness, error) {
		return remote.Readiness{Cores: 8}, nil
	})
	var clean string
	base := doctorIssues(t, &clean)

	fakeProbe(t, func(_ remote.Target, _ []string) (remote.Readiness, error) {
		return remote.Readiness{Cores: 8, Missing: []string{"pnpm"}}, nil
	})
	var withGap string
	if got := doctorIssues(t, &withGap); got != base {
		t.Errorf("a lane's toolchain gap moved the issue count %d -> %d:\n%s", base, got, withGap)
	}

	fakeProbe(t, func(_ remote.Target, _ []string) (remote.Readiness, error) {
		return remote.Readiness{Cores: 8, Missing: []string{"gremlins"}}, nil
	})
	var withAdapter string
	got := doctorIssues(t, &withAdapter)
	if got != base+1 {
		t.Errorf("an adapter's missing tool gave %d issue(s), want %d:\n%s", got, base+1, withAdapter)
	}
	// "there" is load-bearing: the remote line ends "adapter needs it there.",
	// while checkMutationToolchain's LOCAL advisory reads "adapter needs it to
	// measure Go files here." A needle without it is satisfied by the local
	// line on any machine lacking gremlins, so this would pass while the remote
	// attribution it is testing was missing entirely.
	if !strings.Contains(withAdapter, "gremlins adapter needs it there") {
		t.Errorf("the adapter gap lost its attribution:\n%s", withAdapter)
	}
	// A tool the adapter loop never claimed must not fall through it with an
	// empty name — the whole reason that loop is gated on needBy.
	if strings.Contains(withGap, "adapter needs it there") {
		t.Errorf("a lane's missing tool was attributed to an unnamed adapter:\n%s", withGap)
	}
}

// TestDoctorAttributesASharedToolToItsAdapter: when a lane and an adapter want
// the SAME binary, the gap is an issue attributed to the adapter — the adapter
// has no local fallback to take — and the lane's own row still names it.
func TestDoctorAttributesASharedToolToItsAdapter(t *testing.T) {
	doctorLaneFixture(t, "helicon", []string{"gremlins"}, `[[runtime.test_lane]]
name = "mut"
match = ["internal/**"]
command = "gremlins unleash"`)
	fakeProbe(t, func(_ remote.Target, _ []string) (remote.Readiness, error) {
		return remote.Readiness{Cores: 8, Missing: []string{"gremlins"}}, nil
	})

	var out string
	if got := doctorIssues(t, &out); got != 1 {
		t.Errorf("issues = %d, want 1 — the adapter's gap counts, the lane's does not:\n%s", got, out)
	}
	if strings.Contains(out, "the  adapter") {
		t.Errorf("a tool was attributed to an unnamed adapter:\n%s", out)
	}
	row := lineContaining(t, out, "lane mut will run")
	if !strings.Contains(row, "gremlins") || !strings.Contains(row, "helicon") {
		t.Errorf("the lane row does not name the shared binary and the host: %q", row)
	}
}

// TestDoctorProbesOnceForAdaptersAndLanes: a second probe is a second answer,
// and two answers that differ are the drift c-8's "never disagree" clause
// forbids.
func TestDoctorProbesOnceForAdaptersAndLanes(t *testing.T) {
	doctorLaneFixture(t, "helicon", []string{"gremlins"}, doctorGoAndWebLanes)
	var asked []string
	calls := fakeProbe(t, func(_ remote.Target, tools []string) (remote.Readiness, error) {
		asked = append([]string(nil), tools...)
		return remote.Readiness{Cores: 8}, nil
	})

	var out string
	doctorIssues(t, &out)

	if *calls != 1 {
		t.Fatalf("doctor probed %d times, want exactly 1", *calls)
	}
	for _, want := range []string{"go", "pnpm"} {
		if !contains(asked, want) {
			t.Errorf("the probe did not ask for %q: %v", want, asked)
		}
	}
	n := 0
	for _, tool := range asked {
		if tool == "go" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("the gremlins tool and the go lane's tool were probed separately: %v", asked)
	}
}

// TestDoctorReusesTheRunsDerivation: doctor re-deriving a lane's toolchain
// would be a copy of the rule, and a copy drifts — after which doctor's report
// is about a probe set the run does not use.
func TestDoctorReusesTheRunsDerivation(t *testing.T) {
	doctorLaneFixture(t, "helicon", nil, doctorGoAndWebLanes)
	var asked []string
	fakeProbe(t, func(_ remote.Target, tools []string) (remote.Readiness, error) {
		asked = append([]string(nil), tools...)
		return remote.Readiness{Cores: 8}, nil
	})
	var out string
	doctorIssues(t, &out)

	root, err := FindRoot()
	if err != nil {
		t.Fatal(err)
	}
	p, err := project.Load(filepath.Join(root, project.File))
	if err != nil {
		t.Fatal(err)
	}
	for _, lane := range p.Runtime.TestLane {
		for _, tool := range testlane.Toolchain(lane.Command, lane.Prepare, lane.Toolchain) {
			if !contains(asked, tool) {
				t.Errorf("lane %s needs %q and doctor never asked for it: %v", lane.Name, tool, asked)
			}
		}
	}
}

// TestDoctorAndTheRunNameTheSameBinary drives ONE probe answer through both
// surfaces. Either one alone can be self-consistent and still disagree with the
// other, which is the failure c-8 is worded against.
func TestDoctorAndTheRunNameTheSameBinary(t *testing.T) {
	doctorLaneFixture(t, "helicon", nil, doctorGoAndWebLanes)
	fakeProbe(t, func(_ remote.Target, _ []string) (remote.Readiness, error) {
		return remote.Readiness{Cores: 8, Missing: []string{"pnpm"}}, nil
	})
	var doctorOut string
	doctorIssues(t, &doctorOut)
	if !strings.Contains(doctorOut, "pnpm") {
		t.Fatalf("doctor named no missing binary:\n%s", doctorOut)
	}

	grantAllLanes(t)
	if err := GrantConsent(mustRoot(t), "go test ./..."); err != nil {
		t.Fatal(err)
	}
	installLaneLookPath(t)
	installSpawnRecorder(t, nil)
	installRemoteRecorder(t, nil)

	var runOut string
	if err := runCmdCapturing(t, &runOut, Test(), "--files", "web/app.ts"); err != nil {
		t.Fatalf("dross test --files: %v", err)
	}
	fallback := lineContaining(t, runOut, "fallback:")
	if !strings.Contains(fallback, "pnpm") {
		t.Errorf("the run's fallback names a different binary from doctor's report:\n%s\n%s", doctorOut, fallback)
	}
}

// TestDoctorLaneLessOutputIsUnchanged: a repo declaring no lane must read
// exactly as it did before this section existed. Asserted as a golden string,
// because "contains the host" would pass on a line that had grown a field.
func TestDoctorLaneLessOutputIsUnchanged(t *testing.T) {
	doctorLaneFixture(t, "helicon", nil, "")
	fakeProbe(t, func(_ remote.Target, _ []string) (remote.Readiness, error) {
		return remote.Readiness{Cores: 8}, nil
	})

	var out string
	doctorIssues(t, &out)

	const want = "  ✓ helicon reachable — workdir /srv/dross, 8 cores (mutation runs and `dross test`)"
	if !strings.Contains(out, want+"\n") {
		t.Errorf("the reachable line changed shape:\nwant %q\ngot:\n%s", want, out)
	}
	if strings.Contains(out, "lane ") {
		t.Errorf("a repo with no lanes printed a lane row:\n%s", out)
	}
}

// TestDoctorNamesAnUnprobableDerivedToken: the locked first-token rule takes
// `FOO=1 go test` at its word, so a lane can be pinned to local forever by an
// env prefix. Doctor is the only place that is legible, and the remedy is the
// override rather than an install.
func TestDoctorNamesAnUnprobableDerivedToken(t *testing.T) {
	doctorLaneFixture(t, "helicon", nil, `[[runtime.test_lane]]
name = "go"
match = ["internal/**"]
command = "FOO=1 go test ./..."`)
	fakeProbe(t, func(_ remote.Target, tools []string) (remote.Readiness, error) {
		return remote.Readiness{Cores: 8, Missing: tools}, nil
	})

	var out string
	doctorIssues(t, &out)

	if !strings.Contains(out, "FOO=1") {
		t.Errorf("doctor did not name the underivable token:\n%s", out)
	}
	if !strings.Contains(out, "--toolchain") {
		t.Errorf("doctor did not name the override as the fix:\n%s", out)
	}
}

// lineContaining returns the first output line holding sub, failing the test
// when there is none. Assertions here are per LINE: "the output mentions the
// lane and the binary" is satisfied by two unrelated rows.
func lineContaining(t *testing.T, out, sub string) string {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, sub) {
			return line
		}
	}
	t.Fatalf("no line containing %q:\n%s", sub, out)
	return ""
}

func mustRoot(t *testing.T) string {
	t.Helper()
	root, err := FindRoot()
	if err != nil {
		t.Fatal(err)
	}
	return root
}
