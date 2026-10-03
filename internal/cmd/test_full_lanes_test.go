package cmd

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Rivil/dross/internal/project"
)

// fullRunLanes: two lanes, one with a go-package selector, so an unscoped run
// that leaked a derived selector would show it on the go lane's line.
const fullRunLanes = `[[runtime.test_lane]]
name = "go"
match = ["internal/**"]
command = "go test -count=1"
selector = "go-package"

[[runtime.test_lane]]
name = "web"
match = ["web/**"]
command = "npm test"`

// lanesOnlyFixture is a repo with lanes declared and NO runtime.test_command —
// the shape full_run_source is about. No lane is trusted yet.
func lanesOnlyFixture(t *testing.T, lanes string) string {
	t.Helper()
	dir := laneFixture(t)
	appendLanes(t, dir, lanes)
	installLaneLookPath(t)
	p, err := project.Load(filepath.Join(dir, RootDirName, project.File))
	if err != nil {
		t.Fatal(err)
	}
	if p.Runtime.TestCommand != "" {
		t.Fatalf("fixture precondition: want no runtime.test_command, got %q", p.Runtime.TestCommand)
	}
	return dir
}

// TestBareLanesOnlyRunsEveryLane: with no test_command, a bare `dross test`
// runs every lane's own command, unscoped, instead of refusing at the
// whole-suite consent gate that guards a command it would never spawn.
func TestBareLanesOnlyRunsEveryLane(t *testing.T) {
	lanesOnlyFixture(t, fullRunLanes)
	grantAllLanes(t)
	touchFile(t, "internal/a/a.go")
	rec := installSpawnRecorder(t, nil)

	if err := runCmd(t, Test()); err != nil {
		t.Fatalf("a bare run in a lanes-only repo: %v", err)
	}
	if want := []string{"go test -count=1", "npm test"}; !reflect.DeepEqual(rec.lines, want) {
		t.Errorf("spawned %q, want every lane's command with no derived selector %q", rec.lines, want)
	}
}

// TestBareRunHonoursBareTestRun: where test_command is set, the bare run is
// that command byte-for-byte and no lane spawns (locked bare_test_run) — a
// test_command that already runs both lanes must not run them twice.
func TestBareRunHonoursBareTestRun(t *testing.T) {
	filesFixture(t, fullRunLanes)
	grantAllLanes(t)
	rec := installSpawnRecorder(t, nil)

	if err := runCmd(t, Test()); err != nil {
		t.Fatalf("bare run: %v", err)
	}
	if want := []string{"go test ./..."}; !reflect.DeepEqual(rec.lines, want) {
		t.Errorf("spawned %q, want only the consented test_command %q", rec.lines, want)
	}
}

// TestBareLanesOnlyRefusesAnUntrustedLane: each lane still passes its own
// grant. An untrusted lane is skipped and the run exits 6; the trusted one runs.
func TestBareLanesOnlyRefusesAnUntrustedLane(t *testing.T) {
	lanesOnlyFixture(t, fullRunLanes)
	grantLane(t, "web")
	rec := installSpawnRecorder(t, nil)

	err := runCmd(t, Test())
	if got := ExitCode(err); got != exitLaneRefused {
		t.Errorf("exit = %d (%v), want %d for an untrusted lane", got, err, exitLaneRefused)
	}
	if want := []string{"npm test"}; !reflect.DeepEqual(rec.lines, want) {
		t.Errorf("spawned %q, want only the trusted lane %q", rec.lines, want)
	}
}

// TestBareLanesOnlyWorstWins: outcomes fold exactly as on the --files path.
func TestBareLanesOnlyWorstWins(t *testing.T) {
	cases := []struct {
		name    string
		lanes   string
		fail    map[string]error
		want    int
		spawned []string
	}{
		{
			name:    "one green lane, one red",
			lanes:   fullRunLanes,
			fail:    map[string]error{"npm test": errors.New("exit status 1")},
			want:    exitSuiteFailed,
			spawned: []string{"go test -count=1", "npm test"},
		},
		{
			name: "a failed prepare",
			lanes: fullRunLanes + `
prepare = "pnpm install"`,
			fail:    map[string]error{"pnpm install": errors.New("exit status 1")},
			want:    exitPrepareFailed,
			spawned: []string{"go test -count=1", "pnpm install"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			lanesOnlyFixture(t, c.lanes)
			// grantLane covers a lane's prepare too; grantAllLanes would leave
			// the prepared lane's grant stale.
			grantLane(t, "go")
			grantLane(t, "web")
			rec := perLaneSpawn(t, c.fail)

			err := runCmd(t, Test())
			if got := ExitCode(err); got != c.want {
				t.Errorf("exit = %d (%v), want %d", got, err, c.want)
			}
			if !reflect.DeepEqual(rec.lines, c.spawned) {
				t.Errorf("spawned %q, want %q", rec.lines, c.spawned)
			}
		})
	}
}
