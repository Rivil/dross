package ship

import (
	"errors"
	"os"
	"os/exec"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// ghReply is one scripted gh invocation's combined output and exit status.
type ghReply struct {
	out  string
	exit int
}

// scriptGh swaps ghCommand for a fake that answers each call with the next
// reply, and returns a pointer to every argv it was called with. A call past
// the last reply fails the test.
func scriptGh(t *testing.T, replies ...ghReply) *[][]string {
	calls, _ := scriptGhStderr(t, replies...)
	return calls
}

// scriptGhStderr is scriptGh that also captures what gh's failures print to
// ghStderr.
func scriptGhStderr(t *testing.T, replies ...ghReply) (*[][]string, *strings.Builder) {
	t.Helper()
	var calls [][]string
	var stderr strings.Builder
	prevErr := ghStderr
	ghStderr = &stderr
	t.Cleanup(func() { ghStderr = prevErr })
	prev := ghCommand
	ghCommand = func(args ...string) *exec.Cmd {
		calls = append(calls, append([]string(nil), args...))
		if len(calls) > len(replies) {
			t.Fatalf("unexpected gh call #%d: %v", len(calls), args)
		}
		r := replies[len(calls)-1]
		cmd := exec.Command("sh", "-c", `printf '%s' "$OUT"; exit "$CODE"`)
		cmd.Env = append(os.Environ(), "OUT="+r.out, "CODE="+strconv.Itoa(r.exit))
		return cmd
	}
	t.Cleanup(func() { ghCommand = prev })
	return &calls, &stderr
}

var autoMergeOpts = OpenOpts{Provider: "github"}

const (
	armedOut    = "✓ Pull request #12 will be automatically merged via merge when all requirements are met\n"
	mergedOut   = "✓ Merged pull request #12 (chore(dross): bookkeeping)\n"
	cleanOut    = "GraphQL: Pull request Pull request is in clean status (enablePullRequestAutoMerge)\n"
	disabledOut = "GraphQL: Auto merge is not allowed for this repository (enablePullRequestAutoMerge)\n"
)

// Flags ahead of `--`, the PR number behind it (argfence's gh Separator
// policy), and never --admin.
func TestAutoMergeArgv(t *testing.T) {
	for _, method := range []string{"merge", "squash"} {
		calls := scriptGh(t, ghReply{out: armedOut})
		if _, err := AutoMergePR(autoMergeOpts, 12, method); err != nil {
			t.Fatalf("%s: %v", method, err)
		}
		want := []string{"pr", "merge", "--auto", "--" + method, "--", "12"}
		if len(*calls) != 1 || !reflect.DeepEqual((*calls)[0], want) {
			t.Errorf("argv = %v, want %v", *calls, want)
		}
		assertFenced(t, (*calls)[0])
	}
}

// assertFenced checks one gh argv: every flag sits ahead of the `--`
// separator, and --admin appears nowhere.
func assertFenced(t *testing.T, argv []string) {
	t.Helper()
	sep := -1
	for i, a := range argv {
		if a == "--" {
			sep = i
			break
		}
	}
	if sep < 0 {
		t.Errorf("argv %v has no `--` before the PR number", argv)
		return
	}
	for _, a := range argv[sep+1:] {
		if strings.HasPrefix(a, "-") {
			t.Errorf("argv %v has %q behind `--`, where cobra reads it as a positional", argv, a)
		}
	}
	for _, a := range argv {
		if a == "--admin" {
			t.Errorf("argv %v carries --admin, which skips the ruleset", argv)
		}
	}
}

func TestAutoMergeOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name        string
		reply       ghReply
		want        AutoMergeResult
		unavailable bool
		err         bool
	}{
		{"armed", ghReply{out: armedOut}, AutoMergeResult{AutoEnabled: true}, false, false},
		{"merged outright", ghReply{out: mergedOut}, AutoMergeResult{Merged: true}, false, false},
		{"already merged", ghReply{out: "! Pull request #12 was already merged\n"}, AutoMergeResult{Merged: true}, false, false},
		{"auto-merge disabled for the repo", ghReply{out: disabledOut, exit: 1}, AutoMergeResult{}, true, true},
		{"unrecognised success", ghReply{out: "something new\n"}, AutoMergeResult{}, false, true},
		{"other refusal", ghReply{out: "GraphQL: Pull request is not mergeable\n", exit: 1}, AutoMergeResult{}, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls, stderr := scriptGhStderr(t, tc.reply)
			got, err := AutoMergePR(autoMergeOpts, 12, "merge")
			if (err != nil) != tc.err {
				t.Fatalf("err = %v, want error %v", err, tc.err)
			}
			if errors.Is(err, ErrAutoMergeUnavailable) != tc.unavailable {
				t.Errorf("err = %v; ErrAutoMergeUnavailable = %v, want %v", err, errors.Is(err, ErrAutoMergeUnavailable), tc.unavailable)
			}
			if tc.unavailable {
				if !strings.Contains(err.Error(), "auto-merge is not allowed for this repository") {
					t.Errorf("err = %v; must name gh's reason", err)
				}
				if !strings.Contains(stderr.String(), "Auto merge is not allowed") {
					t.Errorf("gh's own words must reach stderr; got %q", stderr.String())
				}
			}
			if got != tc.want {
				t.Errorf("result = %+v, want %+v", got, tc.want)
			}
			if len(*calls) != 1 {
				t.Errorf("gh called %d times, want 1 (no direct-merge attempt): %v", len(*calls), *calls)
			}
		})
	}
}

// GitHub won't arm auto-merge on a PR that is already mergeable; that one
// refusal, and only that one, is followed by exactly one direct merge.
func TestCleanStatusMergesDirectly(t *testing.T) {
	calls := scriptGh(t, ghReply{out: cleanOut, exit: 1}, ghReply{out: mergedOut})
	got, err := AutoMergePR(autoMergeOpts, 12, "merge")
	if err != nil {
		t.Fatal(err)
	}
	if got != (AutoMergeResult{Merged: true}) {
		t.Errorf("result = %+v, want Merged", got)
	}
	if len(*calls) != 2 {
		t.Fatalf("gh called %d times, want 2: %v", len(*calls), *calls)
	}
	if want := []string{"pr", "merge", "--merge", "--", "12"}; !reflect.DeepEqual((*calls)[1], want) {
		t.Errorf("direct merge argv = %v, want %v (no --auto, no --admin)", (*calls)[1], want)
	}
	assertFenced(t, (*calls)[1])

	t.Run("refused direct merge is unavailable", func(t *testing.T) {
		calls, stderr := scriptGhStderr(t, ghReply{out: cleanOut, exit: 1}, ghReply{out: "GraphQL: Repository rule violations found\n", exit: 1})
		got, err := AutoMergePR(autoMergeOpts, 12, "squash")
		if !errors.Is(err, ErrAutoMergeUnavailable) || !strings.Contains(err.Error(), "refused the direct merge") {
			t.Errorf("err = %v, want ErrAutoMergeUnavailable naming the refused direct merge", err)
		}
		if strings.Contains(err.Error(), "Repository rule violations") || !strings.Contains(stderr.String(), "Repository rule violations found") {
			t.Errorf("gh's output belongs on stderr, not in the error: err %v, stderr %q", err, stderr.String())
		}
		if got != (AutoMergeResult{}) || len(*calls) != 2 {
			t.Errorf("result %+v after %d calls, want zero after 2", got, len(*calls))
		}
	})
}

// Bad input is refused before gh runs.
func TestAutoMergeRefusesBadInput(t *testing.T) {
	refuseGh(t)
	for _, tc := range []struct {
		name   string
		opts   OpenOpts
		n      int
		method string
	}{
		{"zero PR", autoMergeOpts, 0, "merge"},
		{"negative PR", autoMergeOpts, -1, "merge"},
		{"admin as a method", autoMergeOpts, 12, "admin"},
		{"empty method", autoMergeOpts, 12, ""},
		{"forgejo", OpenOpts{Provider: "forgejo"}, 12, "merge"},
		{"no provider", OpenOpts{}, 12, "merge"},
	} {
		if _, err := AutoMergePR(tc.opts, tc.n, tc.method); err == nil {
			t.Errorf("%s: accepted", tc.name)
		}
	}
}

func TestDefaultSeams(t *testing.T) {
	if reflect.ValueOf(OpenPRFunc).Pointer() != reflect.ValueOf(OpenPR).Pointer() {
		t.Error("OpenPRFunc must default to OpenPR")
	}
	if reflect.ValueOf(AutoMergePRFunc).Pointer() != reflect.ValueOf(AutoMergePR).Pointer() {
		t.Error("AutoMergePRFunc must default to AutoMergePR")
	}
}
