package mutation

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/remote"
)

// TestScratchRefusesAFullVolume is the incident, inverted into a test: a run
// whose scratch volume is nearly full must refuse BEFORE it writes anything,
// rather than discover the problem by filling the disk.
func TestScratchRefusesAFullVolume(t *testing.T) {
	base := filepath.Join(t.TempDir(), "cache")
	// A floor far above any real free space, which is how you make a
	// tmpdir look full without one.
	t.Setenv(ScratchMinFreeEnv, "999999")

	s, err := newScratch(base, []string{"GOCACHE"}, os.Stderr)
	if err == nil {
		t.Fatal("a full volume must refuse the run")
	}
	if s != nil {
		t.Error("a refusal returned a scratch anyway")
	}
	// Nothing may be created by a run that declined to start.
	if _, statErr := os.Stat(base); !os.IsNotExist(statErr) {
		t.Errorf("the refusal created %s anyway (stat err = %v)", base, statErr)
	}
	for _, want := range []string{base, ScratchBaseEnv, ScratchMinFreeEnv, "GB free"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal must name %q — all three are needed to act on it:\n%v", want, err)
		}
	}
}

func TestScratchRunsWhenThereIsRoom(t *testing.T) {
	base := filepath.Join(t.TempDir(), "cache")
	t.Setenv(ScratchMinFreeEnv, "1")
	s, err := newScratch(base, []string{"GOCACHE"}, os.Stderr)
	if err != nil {
		t.Fatalf("a volume with room must run: %v", err)
	}
	if s.Dir == "" {
		t.Error("no scratch was created")
	}
}

// TestScratchFloorZeroDisables: an operator who knows their volume better than
// a default can must be able to say so.
func TestScratchFloorZeroDisables(t *testing.T) {
	t.Setenv(ScratchMinFreeEnv, "0")
	if err := checkScratchSpace(t.TempDir()); err != nil {
		t.Errorf("0 must disable the floor: %v", err)
	}
}

// TestScratchFloorTypoDoesNotDisable: an unparseable value is a typo, not
// consent to run without a floor. The opposite reading turns a slip into a
// silently disabled guard.
func TestScratchFloorTypoDoesNotDisable(t *testing.T) {
	t.Setenv(ScratchMinFreeEnv, "twenty")
	if got := scratchMinFreeBytes(); got != defaultScratchMinFreeGB*bytesPerGB {
		t.Errorf("floor = %d, want the default — a typo must not disable the check", got)
	}
}

// TestScratchBaseOverrideMovesTheCache is the other half: the operator can
// point the scratch at a chosen volume without relocating the checkout, which
// was the only lever before.
func TestScratchBaseOverrideMovesTheCache(t *testing.T) {
	chosen := t.TempDir()
	t.Setenv(ScratchBaseEnv, chosen)
	got := scratchDirFor("/somewhere/else/myrepo")
	want := filepath.Join(chosen, ".dross-cache", "myrepo")
	if got != want {
		t.Errorf("scratch dir = %q, want %q", got, want)
	}
}

func TestScratchBaseDefaultIsUnchanged(t *testing.T) {
	t.Setenv(ScratchBaseEnv, "")
	got := scratchDirFor("/home/u/myrepo")
	if want := filepath.Join("/home/u", ".dross-cache", "myrepo"); got != want {
		t.Errorf("default placement moved: got %q, want %q", got, want)
	}
}

// TestScratchBaseIgnoresARelativePath: the scratch is later removed wholesale,
// and resolving a relative path against a working directory that varies per
// adapter is how an rm -rf finds somewhere nobody meant.
func TestScratchBaseIgnoresARelativePath(t *testing.T) {
	t.Setenv(ScratchBaseEnv, "some/relative/dir")
	if got := ScratchBaseOverride(); got != "" {
		t.Errorf("override = %q, want it ignored", got)
	}
}

func TestParseDfAvailBytes(t *testing.T) {
	out := "Filesystem     1024-blocks      Used Available Capacity Mounted on\n" +
		"/dev/mapper/x     78620712  71000000   3145728      96% /\n"
	got, ok := parseDfAvailBytes(out)
	if !ok {
		t.Fatal("failed to parse a normal df -Pk")
	}
	if want := uint64(3145728) * 1024; got != want {
		t.Errorf("avail = %d, want %d", got, want)
	}
	if _, ok := parseDfAvailBytes("garbage"); ok {
		t.Error("garbage parsed as a free-space figure")
	}
}

// TestRemoteScratchSpaceRefusesAFullHost is the half that matters most: the
// volume that filled was the HOST's, and a check that only measured the laptop
// would have missed the incident entirely.
func TestRemoteScratchSpaceRefusesAFullHost(t *testing.T) {
	t.Setenv(ScratchMinFreeEnv, "20")
	orig := remoteSpaceProbe
	remoteSpaceProbe = func(_ remote.Target, argv []string) (string, error) {
		if len(argv) == 0 || argv[0] != "df" {
			t.Errorf("probe spawned %v, want a df", argv)
		}
		return "Filesystem 1024-blocks Used Available Capacity Mounted on\n/dev/x 78620712 71000000 3145728 96% /\n", nil
	}
	t.Cleanup(func() { remoteSpaceProbe = orig })

	l := &Launcher{
		Target:    &remote.Target{Host: "h", Workdir: "/home/u/dross"},
		CacheVars: []string{"GOCACHE"},
	}
	err := l.checkRemoteScratchSpace()
	if err == nil {
		t.Fatal("a nearly-full host must refuse the run")
	}
	if !strings.Contains(err.Error(), "h") || !strings.Contains(err.Error(), "remote_scratch_base") {
		t.Errorf("the refusal must name the host and the way to move the cache:\n%v", err)
	}
}

// TestRemoteScratchSpaceAllowsARoomyHost, and asks only once.
func TestRemoteScratchSpaceAsksOnce(t *testing.T) {
	t.Setenv(ScratchMinFreeEnv, "1")
	calls := 0
	orig := remoteSpaceProbe
	remoteSpaceProbe = func(remote.Target, []string) (string, error) {
		calls++
		return "F 1024-blocks Used Available Capacity M\n/dev/x 1 1 999999999 1% /\n", nil
	}
	t.Cleanup(func() { remoteSpaceProbe = orig })

	l := &Launcher{Target: &remote.Target{Host: "h", Workdir: "/home/u/dross"}, CacheVars: []string{"GOCACHE"}}
	for i := 0; i < 3; i++ {
		if err := l.checkRemoteScratchSpace(); err != nil {
			t.Fatalf("a roomy host must run: %v", err)
		}
	}
	if calls != 1 {
		t.Errorf("probed %d times, want once — each extra is a round trip re-learning the same answer", calls)
	}
}

// TestRemoteScratchSpaceUnknownIsNotARefusal: an unanswerable check must not
// become a new way for a run not to happen.
func TestRemoteScratchSpaceUnknownIsNotARefusal(t *testing.T) {
	t.Setenv(ScratchMinFreeEnv, "20")
	orig := remoteSpaceProbe
	remoteSpaceProbe = func(remote.Target, []string) (string, error) {
		return "", errors.New("df: command not found")
	}
	t.Cleanup(func() { remoteSpaceProbe = orig })

	l := &Launcher{Target: &remote.Target{Host: "h", Workdir: "/home/u/dross"}, CacheVars: []string{"GOCACHE"}}
	if err := l.checkRemoteScratchSpace(); err != nil {
		t.Errorf("a df that failed must not refuse the run: %v", err)
	}
}

// TestRemoteScratchBaseMovesTheHostCache pins the remote override.
func TestRemoteScratchBaseMovesTheHostCache(t *testing.T) {
	l := &Launcher{
		Target:    &remote.Target{Host: "h", Workdir: "/home/u/dross", ScratchBase: "/var/lib/buildcache"},
		CacheVars: []string{"GOCACHE"},
	}
	if got, want := l.remoteScratch(), "/var/lib/buildcache/.dross-cache/dross"; got != want {
		t.Errorf("remote scratch = %q, want %q", got, want)
	}
}
