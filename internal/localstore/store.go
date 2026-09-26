// Package localstore owns .dross/local.toml — machine-local values that must
// NOT ride cumulative history: the host allowlist additions, every consent
// grant, the remote-host grant and pool, mutation tuning, the remote env
// allowlist and the detached runs this machine has in flight.
//
// It exists because state.json USED to ride history: state.json was committed
// and every phase squash-merged it onto the base, so a value written there was
// inherited by every later tree on that branch. state.json is gitignored now
// too, but the two stores stay separate — local.toml is typed, hand-editable
// config for machine-local values, state.json is position data dross owns.
//
// Store is the ONE writer of local.toml. Every trust-bearing read goes through
// consent.RefuseTrackedLocal first, so a committed copy is refused unread.
// internal/cmd holds only the `dross local get|set` command tree over it.
package localstore

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/Rivil/dross/internal/consent"
	"github.com/Rivil/dross/internal/remote"
)

// File is the store's basename under .dross/; consent owns the constant because
// the tracked-store refusal names it.
const File = consent.File

// Store is the closed set of machine-local keys. Adding a key here is the
// only way to add one to the store — an unknown key is an error, never a
// silently-written entry that no reader will ever look for.
type Store struct {
	// QuickBase is the branch a standalone `/dross-quick` forked from and
	// committed to. ship and complete reconcile it alongside the phase base so
	// an unpushed .dross chore left on it can't re-seed divergence.
	QuickBase string `toml:"quick_base,omitempty"`

	// AllowHosts is the comma-separated escape hatch for the API host
	// allowlist (internal/hostallow): hosts the derivation from [remote].url
	// plus the built-in SaaS defaults cannot reach.
	//
	// It lives HERE, and only here, on purpose. A committed `allowed_hosts`
	// key in project.toml would be self-authorizing — a hostile .dross/ would
	// set both the api_base and the key permitting it, and the check would be
	// gating config on config. local.toml is authored on one machine and never
	// cloned, so a repo cannot ship its own authorization. ReadAllowHosts
	// protects exactly that property.
	AllowHosts string `toml:"allow_hosts,omitempty"`

	// Grants is every consent this machine has recorded — the trusted_*
	// keys — owned by internal/consent and embedded here so the toml encoder
	// flattens its fields into this file. This struct stays the ONE writer of
	// local.toml; consent reaches it through grantStore below.
	//
	// Every grant key is deliberately ABSENT from Keys: `dross local set`
	// must not be able to grant one. Consent is granted only by `dross trust`
	// (and its --replay/--run/--lane/--lane-install forms), which print the
	// line they are about to trust; a generic key-writer would let an agent
	// grant consent on the user's behalf without ever showing them what for,
	// which is the entire thing being defended against.
	consent.Grants

	// RemoteHost and RemoteWorkdir authorize dross to run this repo's code on
	// another machine — the mutation adapters and, since remote-test-runner,
	// the test suite.
	//
	// Both are deliberately ABSENT from Keys, on exactly the
	// TrustedTestCommand precedent above and for a strictly larger reason:
	// configuring a remote is code execution on a machine of the config's
	// choosing. `dross remote grant` is the only writer, and it prints the host
	// and workdir it is about to authorize BEFORE it writes them. A generic
	// key-writer would let an agent grant that on the user's behalf without
	// ever showing them what for.
	//
	// They live here rather than in project.toml for the same reason
	// allow_hosts does: a committed remote host would be self-authorizing, and
	// project.Load refuses one by name (see the trap fields there).
	RemoteHost    string `toml:"remote_host,omitempty"`
	RemoteWorkdir string `toml:"remote_workdir,omitempty"`

	// RemoteScratchBase puts the HOST's scratch build cache on a chosen
	// volume, for when the granted workdir's parent is not the volume meant
	// for this work.
	//
	// It IS in Keys, unlike the grant beside it. It authorizes nothing —
	// the host was already authorized, and this only says where on that host
	// the cache goes — so the showing-before-writing ceremony that keeps
	// remote_host out of the generic writer does not apply.
	//
	// It exists because the derived default was wrong on the reference host
	// for three months: /home there is part of the 75 GB root LV while the
	// 300 GB volume provisioned for this workload sat elsewhere, and a run
	// filled root at 1.3 GB/min. The only lever was where the checkout lived,
	// which couples cache placement to an unrelated decision.
	RemoteScratchBase string `toml:"remote_scratch_base,omitempty"`

	// RemotePool holds ADDITIONAL authorized hosts, tried in order after the
	// pair above when that one cannot be reached.
	//
	// An array beside the scalars rather than a replacement for them. The grant
	// already carries two generations of keys — the pair, and the deprecated
	// mutation_* aliases kept because local.toml is untracked, so a rename
	// silently stops resolving on every machine that already granted a host and
	// presents as a local run the user believed was remote. A third generation
	// that superseded the scalars would repeat exactly that, so the scalar pair
	// stays authoritative as candidate zero and this only ever adds to it.
	//
	// ABSENT from Keys for the same reason the scalars are: a generic
	// key-writer would let an agent authorize a host without ever showing the
	// user what for.
	RemotePool []RemoteCandidate `toml:"remote_pool,omitempty"`

	// MutationRemoteHost and MutationRemoteWorkdir are the DEPRECATED aliases
	// the same grant used to be written under, kept so an existing local.toml
	// keeps working with nothing re-issued by hand.
	//
	// They are aliases rather than a rename because the grant lives in an
	// untracked file: a clean rename would silently stop resolving on every
	// machine that already granted a host, and the failure would present as a
	// local run the user believed was remote — the exact confusion
	// ReadRemoteGrants's unparseable-store handling exists to prevent.
	//
	// resolveRemoteGrant reads the new keys first; see EffectiveRemote below
	// for why a half-migrated file resolves to the NEW value.
	MutationRemoteHost    string `toml:"mutation_remote_host,omitempty"`
	MutationRemoteWorkdir string `toml:"mutation_remote_workdir,omitempty"`

	// MutationWorkers and MutationTestCPU tune the mutation runner's
	// parallelism: how many mutants run at once, and how many CPUs each
	// mutant's test run may use.
	//
	// These ARE in Keys. They are performance knobs, not authorization —
	// the worst a wrong value does is make a run slow or noisy, which is a
	// different category from granting code execution on another host. Kept
	// machine-local because the right number is a property of the box, not of
	// the repo.
	MutationWorkers string `toml:"mutation_workers,omitempty"`
	MutationTestCPU string `toml:"mutation_test_cpu,omitempty"`

	// MutationRemoteEnv is a comma-separated allowlist of variable NAMES the
	// remote mutation run needs. dross reads each value from its OWN process
	// environment at run time and stores none of them — this key holds names,
	// never name=value pairs.
	//
	// It IS in Keys, and that is the point of the name/value split: names
	// are not secrets, so the key needs none of the grant verb's ceremony, and
	// dross never becomes a place a secret lives. Nothing here is worth
	// stealing, which is the property that makes it safe to ship.
	//
	// An ALLOWLIST rather than forwarding the whole environment, because dross's
	// own environment carries GITHUB_TOKEN and YOUTRACK_TOKEN and "send
	// everything" would put them on the mutation host.
	MutationRemoteEnv string `toml:"mutation_remote_env,omitempty"`

	// DetachedRuns records the mutation runs this machine dispatched to a host
	// and has not yet collected — one per phase, at most.
	//
	// It lives here rather than in the phase directory because it is machine-
	// AND host-local in the same way the grant above is: a run id naming a
	// directory on helicon means nothing in a clone, on another laptop, or to
	// anyone reading the phase's artefacts later. Committing it would also put
	// a host name into cumulative history, which is the self-authorization
	// local.toml exists to keep out.
	//
	// ABSENT from Keys, on the RemotePool precedent one step further: the
	// record names a host AND a directory a later `verify results` will read a
	// report out of, so a generic key-writer could point a fetch at a machine
	// and a path the user never saw. Only the detach verb writes it, and it
	// prints the host it is dispatching to before it does.
	//
	// An ARRAY keyed by phase rather than a map, so the file reads in dispatch
	// order and a hand-inspected local.toml shows what is outstanding without
	// the reader having to know the key set.
	DetachedRuns []DetachedRun `toml:"detached_run,omitempty"`
}

// DetachedRun is one dispatched-but-uncollected mutation run.
//
// RunDir is STORED rather than re-derived from Workdir and RunID at fetch
// time. The derivation is ours and could change; a run dispatched by an older
// dross would then be looked for in a directory nothing ever wrote to, and the
// failure would present as "the run vanished" rather than as a version skew.
// Storing the path the dispatch actually used makes the record self-contained.
//
// ScheduledFor is the zero time for an immediate run. A scheduled one carries
// the instant the host will start it, which is what lets `verify status` say
// "scheduled" rather than "running" without asking the host.
type DetachedRun struct {
	Phase        string    `toml:"phase"`
	RunID        string    `toml:"run_id"`
	Host         string    `toml:"host"`
	Workdir      string    `toml:"workdir"`
	RunDir       string    `toml:"run_dir"`
	DispatchedAt time.Time `toml:"dispatched_at"`
	ScheduledFor time.Time `toml:"scheduled_for,omitempty"`
	State        string    `toml:"state"`
}

// Scheduled reports whether this run is waiting for its start time rather than
// already running.
func (d DetachedRun) Scheduled() bool { return !d.ScheduledFor.IsZero() }

// ReadDetachedRuns returns every dispatched-but-uncollected run.
//
// It goes through consent.RefuseTrackedLocal for the reason ReadRemoteGrants does: the
// record names a host and a path a fetch will read from, so a committed store
// carrying one is a repo pointing this machine's next `verify results` at a
// directory of its choosing. Refused unread, like every other trust-bearing
// read of this file.
//
// An unparseable store is an error rather than an empty list, on the same
// reasoning: "I could not read your config" must not resolve to "you have no
// runs outstanding", because that silently re-dispatches a leg already running
// on the host and leaves two writers for one phase's tests.json.
func ReadDetachedRuns(root, repoDir string) ([]DetachedRun, error) {
	if err := consent.RefuseTrackedLocal(repoDir); err != nil {
		return nil, err
	}
	l, err := Load(Path(root))
	if err != nil {
		return nil, err
	}
	return l.DetachedRuns, nil
}

// FindDetachedRun returns the run recorded for a phase, or (nil, nil) when
// there is none.
func FindDetachedRun(root, repoDir, phaseID string) (*DetachedRun, error) {
	runs, err := ReadDetachedRuns(root, repoDir)
	if err != nil {
		return nil, err
	}
	for i := range runs {
		if runs[i].Phase == phaseID {
			return &runs[i], nil
		}
	}
	return nil, nil
}

// RecordDetachedRun stores a dispatched run, refusing a second one for a phase
// that already has one in flight.
//
// The refusal is the one_run_per_phase decision made mechanical. Two runs
// against one phase both write that phase's tests.json when collected, and the
// loser wins silently — whichever fetch happens second overwrites the first
// with numbers measured from a different dispatch. Refusing by name, with the
// running host in the message, makes cancel-then-redispatch the explicit
// gesture rather than something the user discovers afterwards.
//
// The refusal states the FACT — which run, on which host, dispatched when — and
// narrates no remediation command. The verbs that collect and cancel a run live
// on the verify command, and naming them here binds this layer to the CLI's
// spelling: TestNarratedCommandsResolveAgainstTheTree fails a message naming a
// subcommand that does not exist, and it caught exactly that on the first draft
// of this function. The caller adds the remediation line, where the verbs are.
func RecordDetachedRun(root, repoDir string, rec DetachedRun) error {
	if err := consent.RefuseTrackedLocal(repoDir); err != nil {
		return err
	}
	path := Path(root)
	l, err := Load(path)
	if err != nil {
		return err
	}
	for _, existing := range l.DetachedRuns {
		if existing.Phase == rec.Phase {
			return fmt.Errorf(
				"phase %q already has a detached run in flight: %s on %s (dispatched %s)",
				rec.Phase, existing.RunID, existing.Host,
				existing.DispatchedAt.Format(time.RFC3339))
		}
	}
	l.DetachedRuns = append(l.DetachedRuns, rec)
	return l.Save(path)
}

// ClearDetachedRun removes a phase's record, reporting whether there was one.
//
// The boolean is what lets a cancel of an unknown phase be an error rather
// than a silent success: a caller that cannot tell "removed" from "there was
// nothing" reports both as done, and a user who mistyped a phase id is told
// the run is cancelled while it keeps running on the host.
func ClearDetachedRun(root, repoDir, phaseID string) (bool, error) {
	if err := consent.RefuseTrackedLocal(repoDir); err != nil {
		return false, err
	}
	path := Path(root)
	l, err := Load(path)
	if err != nil {
		return false, err
	}
	kept := make([]DetachedRun, 0, len(l.DetachedRuns))
	found := false
	for _, r := range l.DetachedRuns {
		if r.Phase == phaseID {
			found = true
			continue
		}
		kept = append(kept, r)
	}
	if !found {
		return false, nil
	}
	l.DetachedRuns = kept
	if err := l.Save(path); err != nil {
		return false, err
	}
	return true, nil
}

// Keys maps each key to its accessors, keeping `local get` and
// `local set` agreeing on the key set by construction.
var Keys = map[string]struct {
	Get func(*Store) string
	Set func(*Store, string)
}{
	"quick_base": {
		Get: func(l *Store) string { return l.QuickBase },
		Set: func(l *Store, v string) { l.QuickBase = v },
	},
	"allow_hosts": {
		Get: func(l *Store) string { return l.AllowHosts },
		Set: func(l *Store, v string) { l.AllowHosts = v },
	},
	"mutation_workers": {
		Get: func(l *Store) string { return l.MutationWorkers },
		Set: func(l *Store, v string) { l.MutationWorkers = v },
	},
	"mutation_test_cpu": {
		Get: func(l *Store) string { return l.MutationTestCPU },
		Set: func(l *Store, v string) { l.MutationTestCPU = v },
	},
	"remote_scratch_base": {
		Get: func(l *Store) string { return l.RemoteScratchBase },
		Set: func(l *Store, v string) { l.RemoteScratchBase = v },
	},
	"mutation_remote_env": {
		Get: func(l *Store) string { return l.MutationRemoteEnv },
		Set: func(l *Store, v string) { l.MutationRemoteEnv = v },
	},
	// remote_host and remote_workdir — and their deprecated mutation_remote_*
	// aliases — are NOT here. See the struct fields: they are granted by
	// `dross remote grant`, which shows the user what it is authorizing, and by
	// nothing else. trusted_lane_commands is out for the same reason: only
	// `dross trust --lane <name>` writes it, after printing the line, and
	// trusted_lane_installs is out on that same precedent — a key-writer that
	// could grant it would authorize changing a machine without ever showing
	// the user the line it was about to run there.
}

// EffectiveRemote returns the granted host and workdir, preferring the current
// keys over the deprecated mutation_remote_* aliases.
//
// New-wins is the deliberate direction. A store carrying both is half-migrated
// — someone re-granted through the new verb while the old keys were still on
// disk — and the value they most recently authorized is the new one. Falling
// back the other way would run their code on a box they had already moved off.
//
// The pair is resolved TOGETHER rather than field by field: a host from one
// generation of the file paired with a workdir from the other is a path on a
// machine that was never granted with it.
func (l *Store) EffectiveRemote() (host, workdir string) {
	if l.RemoteHost != "" || l.RemoteWorkdir != "" {
		return l.RemoteHost, l.RemoteWorkdir
	}
	return l.MutationRemoteHost, l.MutationRemoteWorkdir
}

// ReadAllowHosts returns the machine-local host allowlist additions, or an
// error if .dross/local.toml is tracked by git.
//
// The refusal is the point, and it is deliberately not a "parse it but ignore
// allow_hosts" softening. A tracked local.toml is either an accident an honest
// repo wants to know about, or a hostile repo trying to authorize its own
// exfiltration host through the one input the derived allowlist trusts. Reading
// it in either case is wrong, and reading it selectively would still let a
// cloned quick_base ride history — the thing this store was created to stop.
//
// It answers (nil, nil) for a missing or unreadable file: local.toml is
// optional, and a fresh clone legitimately has none. Only "git says this is
// tracked" is an error.
func ReadAllowHosts(root, repoDir string) ([]string, error) {
	if err := consent.RefuseTrackedLocal(repoDir); err != nil {
		return nil, err
	}
	l, err := Load(Path(root))
	if err != nil {
		return nil, nil
	}
	var hosts []string
	for _, h := range strings.Split(l.AllowHosts, ",") {
		if h = strings.TrimSpace(h); h != "" {
			hosts = append(hosts, h)
		}
	}
	return hosts, nil
}

// RemoteCandidate is one authorized host in the pool.
type RemoteCandidate struct {
	Host    string `toml:"host"`
	Workdir string `toml:"workdir"`
}

// ReadRemoteGrants returns every authorized host in preference order: the
// scalar grant first, then the pool. It is the ONE reader of the machine-local
// authorization — nothing else parses local.toml for a host.
//
// Order is the user's declared preference, so honouring it needs no policy of
// our own.
//
// It goes through consent.RefuseTrackedLocal for the same reason the consent gate does,
// and the reason is sharper here: a tracked local.toml naming a remote host is a
// repo shipping the machine it wants your working tree rsync'd to and your test
// suite executed on. The file is refused UNREAD in that case.
//
// The HOST is the authorization, so an absent host is simply no grant — a
// stored workdir on its own is not half a grant, it is nothing, and treating it
// as authorization would be reading intent into a leftover value. A host WITH no
// usable workdir is different: something was authorized and cannot be honoured,
// so Target.Validate refuses it by name rather than falling back.
//
// An unparseable store is an error, not an empty grant. Every other reader of
// this file treats a decode failure as "no value"; a trust-bearing key cannot,
// because "I could not read your config" must never resolve to a silent local
// run the user thought was remote.
func ReadRemoteGrants(root, repoDir string) ([]*remote.Target, error) {
	if err := consent.RefuseTrackedLocal(repoDir); err != nil {
		return nil, err
	}
	l, err := Load(Path(root))
	if err != nil {
		return nil, err
	}
	env, err := ResolveRemoteEnv(l.MutationRemoteEnv)
	if err != nil {
		return nil, err
	}
	var out []*remote.Target
	add := func(host, workdir string) error {
		if host == "" {
			return nil
		}
		t := &remote.Target{Host: host, Workdir: workdir, Env: env, ScratchBase: l.RemoteScratchBase}
		if err := t.Validate(); err != nil {
			return fmt.Errorf("%s: %w", consent.RelPath, err)
		}
		out = append(out, t)
		return nil
	}
	host, workdir := l.EffectiveRemote()
	if err := add(host, workdir); err != nil {
		return nil, err
	}
	for _, c := range l.RemotePool {
		if err := add(c.Host, c.Workdir); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// ResolveRemoteEnv turns the mutation_remote_env NAME allowlist into the
// name/value pairs the remote run needs, reading each value from dross's own
// process environment.
//
// The allowlist is the security property: dross's environment carries
// GITHUB_TOKEN and YOUTRACK_TOKEN, and forwarding everything would put dross's
// own credentials on the mutation host. Only names the user asked for cross.
//
// An allowlisted name that is UNSET is an ERROR, not an empty export. The two
// are not the same thing to the code being measured: a DATABASE_URL that is
// absent and one that is empty select different code paths — different test
// suites load — so an empty export silently changes WHAT gets measured rather
// than failing.
//
// Reading from the process environment rather than parsing a .env file is the
// locked remote_env_source decision. A parser would have to reproduce
// docker-compose's quoting and expansion rules exactly or produce values that
// differ from what the developer sees locally, and it would make dross a thing
// that reads secret files.
func ResolveRemoteEnv(allowlist string) ([]remote.EnvVar, error) {
	var out []remote.EnvVar
	for _, name := range strings.Split(allowlist, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		value, ok := os.LookupEnv(name)
		if !ok {
			return nil, fmt.Errorf(
				"%s: mutation_remote_env names %s, but it is not set in this environment.\n\n"+
					"dross stores no values — it reads each allowlisted name from its own environment at\n"+
					"run time. An unset name is refused rather than exported empty, because empty and\n"+
					"absent select different code paths and would change what the run measures.\n\n"+
					"Export it before running, or drop it from `dross local set mutation_remote_env`.",
				consent.RelPath, name)
		}
		out = append(out, remote.EnvVar{Name: name, Value: value})
	}
	return out, nil
}

// ReadMutationTuning returns the machine-local worker and test-cpu overrides.
//
// Zero means unset, and unset is not the same as zero: the adapters read it as
// "apply your own default" (NumCPU/2 locally, the probed remote count for a
// remote run). Returning 0 for a value the user actually typed would silently
// discard it, so a value that is present but unusable is an error naming the
// key — the user typed something and deserves to hear that it did not take.
func ReadMutationTuning(root string) (workers, testCPU int, err error) {
	l, lerr := Load(Path(root))
	if lerr != nil {
		return 0, 0, lerr
	}
	if workers, err = parseTuning("mutation_workers", l.MutationWorkers); err != nil {
		return 0, 0, err
	}
	if testCPU, err = parseTuning("mutation_test_cpu", l.MutationTestCPU); err != nil {
		return 0, 0, err
	}
	return workers, testCPU, nil
}

func parseTuning(key, raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: %s = %q is not a number", consent.RelPath, key, raw)
	}
	if n < 1 {
		return 0, fmt.Errorf("%s: %s = %d must be at least 1", consent.RelPath, key, n)
	}
	return n, nil
}

// KeyNames lists the settable keys, sorted, for an unknown-key refusal.
func KeyNames() string {
	names := make([]string, 0, len(Keys))
	for k := range Keys {
		names = append(names, k)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// Path is the store's location under a .dross root.
func Path(root string) string { return filepath.Join(root, File) }

// Load reads the store. A missing file is an empty store, not an error —
// the file is gitignored, so a fresh clone legitimately has none.
func Load(path string) (*Store, error) {
	var l Store
	_, err := toml.DecodeFile(path, &l)
	if errors.Is(err, fs.ErrNotExist) {
		return &Store{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	return &l, nil
}

// save writes the store, creating it on demand.
func (l *Store) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	defer f.Close()
	enc := toml.NewEncoder(f)
	enc.Indent = "  "
	if err := enc.Encode(l); err != nil {
		return fmt.Errorf("encode local: %w", err)
	}
	return nil
}

// grantStore is consent.Store over THIS file: it loads the whole
// Store, swaps the embedded Grants, and saves the whole thing back — so
// consent never learns local.toml's layout and every foreign key (quick_base,
// the remote grant, tuning, detached runs) survives a grant untouched.
type grantStore struct{ root string }

func (s grantStore) Load() (*consent.Grants, error) {
	l, err := Load(Path(s.root))
	if err != nil {
		return nil, err
	}
	g := l.Grants
	return &g, nil
}

func (s grantStore) Save(g *consent.Grants) error {
	path := Path(s.root)
	l, err := Load(path)
	if err != nil {
		return err
	}
	l.Grants = *g
	return l.Save(path)
}

// GrantStore is the consent.Store for the tree rooted at root (its .dross).
func GrantStore(root string) consent.Store { return grantStore{root: root} }

// ReadKey is the reader other commands use. Any failure to read the
// store yields an empty value rather than an error: the store is an
// optimisation for reconciliation, never a gate.
func ReadKey(root, key string) string {
	l, err := Load(Path(root))
	if err != nil {
		return ""
	}
	acc, ok := Keys[key]
	if !ok {
		return ""
	}
	return acc.Get(l)
}
