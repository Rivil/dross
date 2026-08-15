package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/project"
	"github.com/Rivil/dross/internal/state"
)

// The c-5 regression suite: one subtest per row of the fixture's refusal
// contract, each reproducing a distinct way a cloned repo's `.dross/` used to
// reach git or the network with the user's credentials.
//
// The contract is parsed from fixtures/hostile-config-c5/expected-refusals.txt
// rather than restated here. That is the point of the file: the strings were
// pinned before the guards existed, so "the test passes" cannot be reached by
// rewording an assertion to fit whatever the code happened to print.

const fixtureDir = "fixtures/hostile-config-c5"

// fixtureRootDir is captured at init, BEFORE any test chdirs into a temp repo.
// Resolving it lazily would make the fixture path depend on whichever fixture
// last called chdir — a failure that only appears when test order changes.
var fixtureRootDir = func() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for i := 0; i < 6; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	return ""
}()

func fixtureRoot(t *testing.T) string {
	t.Helper()
	if fixtureRootDir == "" {
		t.Fatal("could not locate the module root at init")
	}
	return fixtureRootDir
}

// sentinelToken is the fake credential the fixture's auth_env names. No refusal
// may contain it.
const fixtureTokenEnv = "DROSS_FIXTURE_TOKEN"

const fixtureTokenValue = "s3cr3t-fixture-token-do-not-leak"

// vector is one row of expected-refusals.txt.
type vector struct {
	ID      string
	Key     string
	Payload string
	Expect  []string
}

// loadVectors parses the refusal contract. The parser is strict — a malformed
// row is a fatal error, not a skipped line — because a row silently dropped is
// a vector silently untested.
func loadVectors(t *testing.T) []vector {
	t.Helper()
	path := filepath.Join(fixtureRoot(t), fixtureDir, "expected-refusals.txt")
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open refusal contract: %v", err)
	}
	defer f.Close()

	var out []vector
	sc := bufio.NewScanner(f)
	for line := 1; sc.Scan(); line++ {
		raw := strings.TrimSpace(sc.Text())
		if raw == "" || strings.HasPrefix(raw, "#") {
			continue
		}
		parts := strings.Split(raw, "|")
		if len(parts) != 4 {
			t.Fatalf("%s:%d: want 4 '|'-separated fields, got %d: %q", path, line, len(parts), raw)
		}
		v := vector{
			ID:      strings.TrimSpace(parts[0]),
			Key:     strings.TrimSpace(parts[1]),
			Payload: strings.TrimSpace(parts[2]),
		}
		for _, e := range strings.Split(parts[3], ";;") {
			if e = strings.TrimSpace(e); e != "" {
				v.Expect = append(v.Expect, e)
			}
		}
		if v.ID == "" || len(v.Expect) == 0 {
			t.Fatalf("%s:%d: row has no id or no expectations: %q", path, line, raw)
		}
		out = append(out, v)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if len(out) == 0 {
		t.Fatalf("%s parsed to zero vectors — the suite would pass vacuously", path)
	}
	return out
}

// fixtureRepo copies the hostile fixture into a throwaway repo's .dross/,
// substituting __SENTINEL__ with a path inside the test's temp dir, and applies
// the vector's payload. Returns the repo dir and the sentinel path.
//
// The config arrives as a FILE, not through `dross project set` — that is how it
// reaches a real user, and a value routed through the setter would be validated
// on the way in, testing a path the attacker never takes.
func fixtureRepo(t *testing.T, v vector) (repoDir, sentinel string) {
	t.Helper()
	sentinel = filepath.Join(t.TempDir(), "dross-pwned")

	src := filepath.Join(fixtureRoot(t), fixtureDir, "project.toml")
	body, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read fixture project.toml: %v", err)
	}

	repoDir = t.TempDir()
	remote := t.TempDir()
	mustGit(t, remote, "init", "-q", "--bare", "-b", "main")
	gitInit(t, repoDir, remote)
	chdir(t, repoDir)
	t.Setenv(fixtureTokenEnv, fixtureTokenValue)

	root := filepath.Join(repoDir, ".dross")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, project.File),
		[]byte(strings.ReplaceAll(string(body), "__SENTINEL__", sentinel)), 0o644); err != nil {
		t.Fatal(err)
	}
	// state.json is NOT in RequiredRootFiles, so writeCompleteRoot does not
	// seed it — every command that reads position data needs one here.
	if err := state.New().Save(filepath.Join(root, state.File)); err != nil {
		t.Fatal(err)
	}
	// rules.toml is doctor's own notion of foundational: without it, doctor
	// reports the missing file and returns BEFORE the config-trust sections,
	// so the doctor vectors would silently assert on the wrong output.
	if err := os.WriteFile(filepath.Join(root, "rules.toml"), []byte("\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeCompleteRoot(t, root)

	// The row's payload, where it overrides what the fixture committed.
	if v.Payload != "-" {
		payload := strings.ReplaceAll(v.Payload, "__SENTINEL__", sentinel)
		switch v.Key {
		case "repo.git_main_branch", "repo.branch_pattern", "remote.api_base", "board.base_url":
			ppath := filepath.Join(root, project.File)
			p, err := project.Load(ppath)
			if err != nil {
				t.Fatal(err)
			}
			switch v.Key {
			case "repo.git_main_branch":
				p.Repo.GitMainBranch = payload
			case "repo.branch_pattern":
				p.Repo.BranchPattern = payload
			case "remote.api_base":
				p.Remote.APIBase = payload
			case "board.base_url":
				p.Board.BaseURL = payload
			}
			if err := p.Save(ppath); err != nil {
				t.Fatal(err)
			}
		case "argv[0]":
			// Applied by the driver, not to the config.
		default:
			t.Fatalf("vector %s: unknown key %q — add a case here rather than letting the payload be silently dropped", v.ID, v.Key)
		}
	}

	// The tracked-local vector needs the attacker's committed local.toml.
	if v.Key == ".dross/"+LocalFile {
		lsrc := filepath.Join(fixtureRoot(t), fixtureDir, "local-tracked.toml")
		lbody, err := os.ReadFile(lsrc)
		if err != nil {
			t.Fatalf("read fixture local-tracked.toml: %v", err)
		}
		if err := os.WriteFile(filepath.Join(root, LocalFile), lbody, 0o644); err != nil {
			t.Fatal(err)
		}
		mustGit(t, repoDir, "add", "-f", ".dross/"+LocalFile)
	}

	mustWrite(t, filepath.Join(repoDir, "README.md"), "base\n")
	mustGit(t, repoDir, "add", "README.md")
	mustGit(t, repoDir, "commit", "-q", "-m", "chore: baseline")

	// A current phase, so the phase-scoped vectors have something to act on.
	st, err := state.Load(filepath.Join(root, state.File))
	if err != nil {
		t.Fatal(err)
	}
	st.CurrentPhase = "01-x"
	if err := st.Save(filepath.Join(root, state.File)); err != nil {
		t.Fatal(err)
	}

	// `dross repair` only reconstructs state — and so only reaches
	// `git log <mainBranch>` — when it judges state.json stale, which it does
	// by comparing the current phase/<id> branch against state.current_phase.
	// Without that mismatch the command reports "nothing to repair" and the
	// vector would assert on a code path the payload never touches.
	if v.ID == "repair-state" {
		mustGit(t, repoDir, "checkout", "-q", "-b", "phase/other")
	}
	return repoDir, sentinel
}

// vectorDrivers maps each vector id to the thing it drives. Every driver
// returns the combined stdout + error text the row's expectations match
// against: some vectors refuse (an error) and the doctor ones report (output),
// and both are "what the user is told".
var vectorDrivers = map[string]func(t *testing.T, repoDir string, v vector) string{
	"phase-complete": func(t *testing.T, _ string, v vector) string {
		return runVector(t, func() error { return runCmd(t, Phase(), "complete", "01-x") })
	},
	"phase-checkout": func(t *testing.T, _ string, v vector) string {
		payload := strings.ReplaceAll(v.Payload, "__SENTINEL__", "/tmp/dross-pwned")
		return runVector(t, func() error { return runCmd(t, Phase(), "checkout", "--", payload) })
	},
	"phase-create": func(t *testing.T, _ string, v vector) string {
		return runVector(t, func() error { return runCmd(t, Phase(), "create", "hostile phase") })
	},
	"milestone-create": func(t *testing.T, _ string, v vector) string {
		return runVector(t, func() error { return runCmd(t, Milestone(), "create", "v9.9") })
	},
	"ship-recover": func(t *testing.T, _ string, v vector) string {
		return runVector(t, func() error { return runCmd(t, Ship(), "recover") })
	},
	"repair-state": func(t *testing.T, _ string, v vector) string {
		return runVector(t, func() error { return runCmd(t, Repair()) })
	},
	"board-client": func(t *testing.T, repoDir string, v vector) string {
		return runVector(t, func() error {
			p, err := project.Load(filepath.Join(repoDir, ".dross", project.File))
			if err != nil {
				return err
			}
			cfg := boardConfig(p.Board, p.Remote.URL, nil)
			return cfg.Hosts.Check("[board].base_url", cfg.APIBase)
		})
	},
	"ship-open": func(t *testing.T, repoDir string, v vector) string {
		return runVector(t, func() error {
			p, err := project.Load(filepath.Join(repoDir, ".dross", project.File))
			if err != nil {
				return err
			}
			hosts, herr := remotePolicy(filepath.Join(repoDir, ".dross"), repoDir, p)
			if herr != nil {
				return herr
			}
			return hosts.Check("[remote].api_base", p.Remote.APIBase)
		})
	},
	"tracked-local": func(t *testing.T, repoDir string, v vector) string {
		return runVector(t, func() error {
			_, err := readAllowHosts(filepath.Join(repoDir, ".dross"), repoDir)
			return err
		})
	},
	"doctor-branch":         doctorVector,
	"doctor-branch-pattern": doctorVector,
	"doctor-host":           doctorVector,
}

func doctorVector(t *testing.T, _ string, _ vector) string {
	return runVector(t, func() error { return runCmd(t, Doctor()) })
}

// runVector captures both channels: a refusal is an error, a doctor finding is
// printed, and the contract's expectations are about what reaches the user
// either way.
func runVector(t *testing.T, fn func() error) string {
	t.Helper()
	var err error
	out := captureStdout(t, func() { err = fn() })
	if err != nil {
		out += "\n" + err.Error()
	}
	return out
}

// TestHostileConfigVectors is the suite. Table-driven off the pinned contract,
// with per-vector side-effect assertions that no error string could satisfy.
func TestHostileConfigVectors(t *testing.T) {
	for _, v := range loadVectors(t) {
		t.Run(v.ID, func(t *testing.T) {
			drive, ok := vectorDrivers[v.ID]
			if !ok {
				t.Fatalf("vector %q has no driver — a row without one is a vector nobody tests", v.ID)
			}
			repoDir, sentinel := fixtureRepo(t, v)

			got := drive(t, repoDir, v)

			for _, want := range v.Expect {
				if !strings.Contains(got, strings.ReplaceAll(want, "__SENTINEL__", sentinel)) {
					t.Errorf("output does not contain %q:\n%s", want, got)
				}
			}
			// The assertions no wording can fake.
			if _, err := os.Stat(sentinel); err == nil {
				t.Errorf("the payload landed: %s exists", sentinel)
			}
			if strings.Contains(got, fixtureTokenValue) {
				t.Errorf("the token leaked into what the user was shown:\n%s", got)
			}
		})
	}
}

// TestEveryVectorHasASubtest makes the contract and the suite each other's
// gate: a row deleted to green the suite fails here, and a driver added without
// a row fails here too.
func TestEveryVectorHasASubtest(t *testing.T) {
	var rows, drivers []string
	for _, v := range loadVectors(t) {
		rows = append(rows, v.ID)
	}
	for id := range vectorDrivers {
		drivers = append(drivers, id)
	}
	sort.Strings(rows)
	sort.Strings(drivers)
	if strings.Join(rows, ",") != strings.Join(drivers, ",") {
		t.Errorf("vector ids and subtest drivers differ:\n  contract: %v\n  drivers:  %v", rows, drivers)
	}
}

// TestHostileConfigNoPwnSentinel is the whole-run backstop. Each vector already
// checks its own sentinel, but a payload could land through a path no single
// vector attributes to itself — so the fixture's committed shapes are run once
// more against a shared sentinel and the file must still not exist.
func TestHostileConfigNoPwnSentinel(t *testing.T) {
	// The fixture must still CARRY a sentinel-writing payload, or every
	// assertion above is checking that a harmless config wrote no file.
	body, err := os.ReadFile(filepath.Join(fixtureRoot(t), fixtureDir, "project.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "--output=__SENTINEL__") {
		t.Fatal("the fixture no longer carries the --output= payload — the sentinel assertions are vacuous")
	}

	for _, v := range loadVectors(t) {
		t.Run(v.ID, func(t *testing.T) {
			repoDir, sentinel := fixtureRepo(t, v)
			if drive, ok := vectorDrivers[v.ID]; ok {
				drive(t, repoDir, v)
			}
			if _, err := os.Stat(sentinel); err == nil {
				t.Errorf("%s exists after vector %s", sentinel, v.ID)
			}
		})
	}
}

// TestRedProofPinsBaseCommit keeps RUN.md honest. The red replay is only worth
// anything if someone else can re-run it, which means a real base commit and a
// named failing test per vector — not prose describing that it was done.
func TestRedProofPinsBaseCommit(t *testing.T) {
	root := fixtureRoot(t)
	body, err := os.ReadFile(filepath.Join(root, fixtureDir, "RUN.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)

	if strings.Contains(text, "NOT YET RECORDED") {
		t.Fatal("RUN.md still carries the placeholder — the red replay was never recorded")
	}

	// The pin is read through discovery, not by re-parsing this doc's path: the
	// record is what doctor checks, and a suite reading a different source
	// could stay green over a pin doctor calls rotted.
	pin := fixtureRedProofPin(t, root)
	if docSHA := redProofSHA(text); docSHA != pin.SHA {
		t.Errorf("RUN.md says base commit %q but %s's record pins %q — fix one to match the other (`dross phase red-proof set`)",
			docSHA, pin.Phase, pin.SHA)
	}

	// Reachability is decided by the SAME classifier doctor runs (c-4). A
	// shallow clone reports cannot-determine, and that stays a skip-with-log:
	// an absence of history is not a dishonest RUN.md, and reddening on it
	// would be a false red. ci.yml fetches full history for exactly this
	// assertion, so the log line is a signal that some *other* environment
	// truncated the clone, never a silently-disabled check in ours.
	verdict, why, err := classifyReachability(root, pin.SHA)
	switch {
	case err != nil:
		t.Errorf("could not check the pin %s: %v", pin.SHA, err)
	case verdict == reachIndeterminate:
		t.Logf("cannot verify pinned base commit %s — %s (ci.yml fetches full history so this check still runs in CI)", pin.SHA, why)
	case verdict == reachUnreachable:
		t.Errorf("%s pins base commit %s, which is unreachable — %s. Repoint it to the phase's fork point %s",
			pin.Doc, pin.SHA, why, fixtureForkPoint(t, root, pin.Phase))
	}

	for _, v := range loadVectors(t) {
		if !strings.Contains(text, v.ID) {
			t.Errorf("RUN.md's replay does not mention vector %q", v.ID)
		}
	}
}

// fixtureRedProofPin finds this fixture's pin through the same discovery doctor
// uses. A missing pin is fatal: the fixture's whole claim is that the replay is
// re-runnable, and an unrecorded proof is one nothing checks.
func fixtureRedProofPin(t *testing.T, root string) redProofPin {
	t.Helper()
	pins, err := discoverRedProofPins(filepath.Join(root, RootDirName))
	if err != nil {
		t.Fatalf("discover red-proof pins: %v", err)
	}
	want := fixtureDir + "/RUN.md"
	for _, p := range pins {
		if p.Doc == want {
			return p
		}
	}
	t.Fatalf("no phase records a red proof for %s — record it with `dross phase red-proof set <phase> --sha <sha> --doc %s`", want, want)
	return redProofPin{}
}

// fixtureForkPoint renders the commit a rotted pin should be repointed to, or
// why it could not be resolved. It never fails the test on its own: the finding
// is the rotted pin, and losing that message to a secondary failure would be
// the worse outcome.
func fixtureForkPoint(t *testing.T, root, phaseID string) string {
	t.Helper()
	fork, err := phaseForkPoint(root, filepath.Join(root, RootDirName), phaseID)
	if err != nil {
		return fmt.Sprintf("(unresolved: %v)", err)
	}
	return fork
}

// TestRedProofCheckHasOneCaller is the delegation gate for c-4. The reachability
// rule must have ONE implementation: this file may read the doc and the record,
// but the moment it asks git a containment question of its own, the suite and
// doctor can disagree about whether a pin is sound — which is the exact split
// that let the c-5 pin pass here and rot for everyone else.
func TestRedProofCheckHasOneCaller(t *testing.T) {
	body, err := os.ReadFile(filepath.Join(fixtureRoot(t), "internal", "cmd", "hostile_config_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	// Built by concatenation so this gate does not match its own probe list.
	for _, probe := range []string{"rev-" + "parse", "for-each-" + "ref", "merge-" + "base", "is-shallow-" + "repository"} {
		if strings.Contains(string(body), probe) {
			t.Errorf("hostile_config_test.go asks git %q itself — reachability must go through classifyReachability so this suite and doctor cannot disagree", probe)
		}
	}
	if !strings.Contains(string(body), "classifyReachability(") {
		t.Error("hostile_config_test.go no longer calls classifyReachability — the fixture check has stopped delegating")
	}
}

// The pin parser used to live here. It moved to redproof.go so doctor and this
// suite read a doc's pin through the same code (c-4) — a second copy would be a
// second answer to "what does this doc pin", and the two would drift.
