package cmd

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
	"github.com/spf13/cobra"

	"github.com/Rivil/dross/internal/remote"
)

// Local manages .dross/local.toml — machine-local values that must NOT ride
// cumulative history.
//
// It exists because state.json USED to ride it: state.json was committed and
// every phase squash-merged it onto the base, so a value written there was
// inherited by every later tree on that branch. For a "which branch did this
// work fork from?" record that is exactly wrong — a stale answer from one
// machine's standalone quick task would be dragged forward and later
// reconciled against.
//
// state.json is gitignored now too (locked state_tracking), for the same
// reason plus a worse one: a checkout could replay a branch's stale copy over
// the live file. The two stores stay separate anyway — local.toml is typed,
// hand-editable config for machine-local values, state.json is position data
// dross owns.
//
// Phase work does not use this store — a phase's forked-from base lives in its
// phase-scoped changes.json, which cannot be dragged forward either.
func Local() *cobra.Command {
	c := &cobra.Command{
		Use:   "local",
		Short: "Read and write .dross/local.toml (gitignored, machine-local values)",
	}
	c.AddCommand(localGet(), localSet())
	return c
}

// LocalFile is the store's name under .dross/.
const LocalFile = "local.toml"

// localStore is the closed set of machine-local keys. Adding a key here is the
// only way to add one to the store — an unknown key is an error, never a
// silently-written entry that no reader will ever look for.
type localStore struct {
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
	// cloned, so a repo cannot ship its own authorization. readAllowHosts
	// protects exactly that property.
	AllowHosts string `toml:"allow_hosts,omitempty"`

	// TrustedTestCommand is sha256(runtime.test_command) for the command the
	// user consented to dross spawning — see trust.go for the whole gate.
	//
	// It is deliberately ABSENT from localKeys: `dross local set` must not be
	// able to grant it. Consent is granted only by `dross trust`, which prints
	// the command it is about to trust; a generic key-writer would let an agent
	// grant consent on the user's behalf without ever showing them what for,
	// which is the entire thing being defended against.
	TrustedTestCommand string `toml:"trusted_test_command,omitempty"`

	// TrustedReplayCommands is the comma-separated set of sha256 fingerprints
	// for the red-proof replay commands this machine has consented to dross
	// spawning — see redproof_replay.go for what runs them.
	//
	// A separate key rather than a reuse of TrustedTestCommand because the two
	// are different grants: the test command is one line from project.toml, a
	// replay line is one per phase and arrives from changes.json, which is
	// TRACKED. A cloned repo can therefore propose the command; consenting to
	// spawn it is code execution chosen by the repo, so it needs the same
	// showing-before-writing ceremony the test command gets.
	//
	// ABSENT from localKeys, on the TrustedTestCommand precedent: `dross local
	// set` must not be able to grant it. Only `dross trust --replay <phase-id>`
	// writes it, and it prints the line first.
	TrustedReplayCommands string `toml:"trusted_replay_commands,omitempty"`

	// TrustedRunCommands is the comma-separated set of sha256 fingerprints for
	// the [runtime] slot commands this machine has consented to `dross run`
	// spawning.
	//
	// A third grant rather than a reuse of TrustedTestCommand, for the reason
	// the binding exists at all: a grant covering "whatever [runtime] happens
	// to say" would let a dev_command arriving in a pull inherit trust for a
	// line nobody read. project.toml is TRACKED, so the repo proposes these
	// commands; consenting to spawn one is code execution the repo chose.
	//
	// A SET, like the replay grant: a repo has many runtime slots and granting
	// `dross run dev` must not silently revoke `dross run migrate`.
	//
	// ABSENT from localKeys on the same precedent — only `dross trust --run
	// <name>` writes it, and it prints the line first.
	TrustedRunCommands string `toml:"trusted_run_commands,omitempty"`

	// TrustedLaneCommands maps a [[runtime.test_lane]] name to sha256 of that
	// lane's command line — the per-lane half of the exec-consent gate.
	//
	// A MAP keyed by lane name, not a comma-separated fingerprint set like the
	// replay and run grants beside it. The set shape answers "has this exact
	// line been trusted?", which is enough when the lines are independent. A
	// lane's grant has to answer a second question the set cannot: WHICH lane
	// went stale. With an aggregate or an anonymous set, a one-character edit
	// to a docs lane's command is indistinguishable from an edit to the Go
	// lane's, so the gate can only refuse the whole run — and a docs typo that
	// blocks the Go test gate is a gate people route around. Keyed by name,
	// one stale lane refuses only itself (the locked lane_consent decision).
	//
	// The name is the key, so a RENAMED lane inherits nothing: the lookup
	// misses and the new name is simply ungranted. That is the correct
	// direction — a rename is an edit to the lane, and every edit re-prompts.
	//
	// ABSENT from localKeys, on the TrustedTestCommand precedent: `dross local
	// set` must not be able to grant it. Only `dross trust --lane <name>`
	// writes it, and it prints the command line first.
	TrustedLaneCommands map[string]string `toml:"trusted_lane_commands,omitempty"`

	// TrustedLaneInstalls maps a [[runtime.test_lane]] name to sha256 of that
	// lane's declared `install` line — consent to INSTALL that lane's toolchain,
	// which is a different act from consent to run its suite.
	//
	// A second map rather than a second line folded into TrustedLaneCommands'
	// fingerprint, and that separation is the locked install_consent decision.
	// Folding it in the way `prepare` is folded in would staleness-refuse a
	// lane's ordinary TEST runs the moment an install line was added — a line
	// that has never executed breaking a gate that was passing the day before.
	// The blast radius differs too: running a suite touches this repo's tree,
	// while installing changes a machine for everything else that uses it.
	//
	// Keyed by lane name on the TrustedLaneCommands precedent, so one lane's
	// rewritten install line refuses only itself, and a renamed lane inherits
	// nothing.
	//
	// ABSENT from localKeys, for the reason every grant here is: `dross local
	// set` must not be able to authorize an install. Only `dross trust
	// --lane-install <name>` writes it, and it prints the line first.
	TrustedLaneInstalls map[string]string `toml:"trusted_lane_installs,omitempty"`

	// RemoteHost and RemoteWorkdir authorize dross to run this repo's code on
	// another machine — the mutation adapters and, since remote-test-runner,
	// the test suite.
	//
	// Both are deliberately ABSENT from localKeys, on exactly the
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
	// It IS in localKeys, unlike the grant beside it. It authorizes nothing —
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
	// ABSENT from localKeys for the same reason the scalars are: a generic
	// key-writer would let an agent authorize a host without ever showing the
	// user what for.
	RemotePool []remoteCandidate `toml:"remote_pool,omitempty"`

	// MutationRemoteHost and MutationRemoteWorkdir are the DEPRECATED aliases
	// the same grant used to be written under, kept so an existing local.toml
	// keeps working with nothing re-issued by hand.
	//
	// They are aliases rather than a rename because the grant lives in an
	// untracked file: a clean rename would silently stop resolving on every
	// machine that already granted a host, and the failure would present as a
	// local run the user believed was remote — the exact confusion
	// readRemoteGrant's unparseable-store handling exists to prevent.
	//
	// resolveRemoteGrant reads the new keys first; see effectiveRemote below
	// for why a half-migrated file resolves to the NEW value.
	MutationRemoteHost    string `toml:"mutation_remote_host,omitempty"`
	MutationRemoteWorkdir string `toml:"mutation_remote_workdir,omitempty"`

	// MutationWorkers and MutationTestCPU tune the mutation runner's
	// parallelism: how many mutants run at once, and how many CPUs each
	// mutant's test run may use.
	//
	// These ARE in localKeys. They are performance knobs, not authorization —
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
	// It IS in localKeys, and that is the point of the name/value split: names
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
	// ABSENT from localKeys, on the RemotePool precedent one step further: the
	// record names a host AND a directory a later `verify results` will read a
	// report out of, so a generic key-writer could point a fetch at a machine
	// and a path the user never saw. Only the detach verb writes it, and it
	// prints the host it is dispatching to before it does.
	//
	// An ARRAY keyed by phase rather than a map, so the file reads in dispatch
	// order and a hand-inspected local.toml shows what is outstanding without
	// the reader having to know the key set.
	DetachedRuns []detachedRun `toml:"detached_run,omitempty"`
}

// detachedRun is one dispatched-but-uncollected mutation run.
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
type detachedRun struct {
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
func (d detachedRun) Scheduled() bool { return !d.ScheduledFor.IsZero() }

// readDetachedRuns returns every dispatched-but-uncollected run.
//
// It goes through refuseTrackedLocal for the reason readRemoteGrant does: the
// record names a host and a path a fetch will read from, so a committed store
// carrying one is a repo pointing this machine's next `verify results` at a
// directory of its choosing. Refused unread, like every other trust-bearing
// read of this file.
//
// An unparseable store is an error rather than an empty list, on the same
// reasoning: "I could not read your config" must not resolve to "you have no
// runs outstanding", because that silently re-dispatches a leg already running
// on the host and leaves two writers for one phase's tests.json.
func readDetachedRuns(root, repoDir string) ([]detachedRun, error) {
	if err := refuseTrackedLocal(repoDir); err != nil {
		return nil, err
	}
	l, err := loadLocal(localPath(root))
	if err != nil {
		return nil, err
	}
	return l.DetachedRuns, nil
}

// findDetachedRun returns the run recorded for a phase, or (nil, nil) when
// there is none.
func findDetachedRun(root, repoDir, phaseID string) (*detachedRun, error) {
	runs, err := readDetachedRuns(root, repoDir)
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

// recordDetachedRun stores a dispatched run, refusing a second one for a phase
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
func recordDetachedRun(root, repoDir string, rec detachedRun) error {
	if err := refuseTrackedLocal(repoDir); err != nil {
		return err
	}
	path := localPath(root)
	l, err := loadLocal(path)
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
	return l.save(path)
}

// clearDetachedRun removes a phase's record, reporting whether there was one.
//
// The boolean is what lets a cancel of an unknown phase be an error rather
// than a silent success: a caller that cannot tell "removed" from "there was
// nothing" reports both as done, and a user who mistyped a phase id is told
// the run is cancelled while it keeps running on the host.
func clearDetachedRun(root, repoDir, phaseID string) (bool, error) {
	if err := refuseTrackedLocal(repoDir); err != nil {
		return false, err
	}
	path := localPath(root)
	l, err := loadLocal(path)
	if err != nil {
		return false, err
	}
	kept := make([]detachedRun, 0, len(l.DetachedRuns))
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
	if err := l.save(path); err != nil {
		return false, err
	}
	return true, nil
}

// localKeys maps each key to its accessors, keeping `local get` and
// `local set` agreeing on the key set by construction.
var localKeys = map[string]struct {
	get func(*localStore) string
	set func(*localStore, string)
}{
	"quick_base": {
		get: func(l *localStore) string { return l.QuickBase },
		set: func(l *localStore, v string) { l.QuickBase = v },
	},
	"allow_hosts": {
		get: func(l *localStore) string { return l.AllowHosts },
		set: func(l *localStore, v string) { l.AllowHosts = v },
	},
	"mutation_workers": {
		get: func(l *localStore) string { return l.MutationWorkers },
		set: func(l *localStore, v string) { l.MutationWorkers = v },
	},
	"mutation_test_cpu": {
		get: func(l *localStore) string { return l.MutationTestCPU },
		set: func(l *localStore, v string) { l.MutationTestCPU = v },
	},
	"remote_scratch_base": {
		get: func(l *localStore) string { return l.RemoteScratchBase },
		set: func(l *localStore, v string) { l.RemoteScratchBase = v },
	},
	"mutation_remote_env": {
		get: func(l *localStore) string { return l.MutationRemoteEnv },
		set: func(l *localStore, v string) { l.MutationRemoteEnv = v },
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

// effectiveRemote returns the granted host and workdir, preferring the current
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
func (l *localStore) effectiveRemote() (host, workdir string) {
	if l.RemoteHost != "" || l.RemoteWorkdir != "" {
		return l.RemoteHost, l.RemoteWorkdir
	}
	return l.MutationRemoteHost, l.MutationRemoteWorkdir
}

// readAllowHosts returns the machine-local host allowlist additions, or an
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
func readAllowHosts(root, repoDir string) ([]string, error) {
	if err := refuseTrackedLocal(repoDir); err != nil {
		return nil, err
	}
	l, err := loadLocal(localPath(root))
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

// refuseTrackedLocal is the provenance check every reader of a trust-bearing
// key in local.toml goes through, extracted so readAllowHosts and the exec
// consent gate share ONE refusal rather than two that drift apart.
//
// It returns nil for a missing or untracked file — local.toml is optional, and
// a fresh clone legitimately has none. Only "git says this is tracked" is an
// error, and the file is refused UNREAD in that case: a committed store is
// either an accident an honest repo wants to know about, or a hostile repo
// authorizing itself through the one input dross trusts precisely because it is
// never cloned.
func refuseTrackedLocal(repoDir string) error {
	rel := RootDirName + "/" + LocalFile
	if gitNoOut(repoDir, "ls-files", "--error-unmatch", "--", rel) != nil {
		return nil
	}
	return fmt.Errorf(
		"refusing to read %s: git reports it tracked.\n\n"+
			"%s is machine-local by design — it is where this machine records what it\n"+
			"trusts (API allowlist hosts, the consented test command), so a committed\n"+
			"copy would let the repo authorize itself. dross will not read a tracked one.\n\n"+
			"To fix, untrack it and keep the local copy:\n\n"+
			"    git rm --cached %s\n"+
			"    git commit -m \"chore: untrack dross local store\"",
		rel, rel, rel)
}

// readRemoteGrant returns the machine-local authorization for a remote mutation
// run, or (nil, nil) when the repo has none.
//
// It goes through refuseTrackedLocal for the same reason the consent gate does,
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
// remoteCandidate is one authorized host in the pool.
type remoteCandidate struct {
	Host    string `toml:"host"`
	Workdir string `toml:"workdir"`
}

// readRemoteGrants returns every authorized host in preference order: the
// scalar grant first, then the pool.
//
// Order is the user's declared preference, so honouring it needs no policy of
// our own.
func readRemoteGrants(root, repoDir string) ([]*remote.Target, error) {
	if err := refuseTrackedLocal(repoDir); err != nil {
		return nil, err
	}
	l, err := loadLocal(localPath(root))
	if err != nil {
		return nil, err
	}
	env, err := resolveRemoteEnv(l.MutationRemoteEnv)
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
			return fmt.Errorf("%s/%s: %w", RootDirName, LocalFile, err)
		}
		out = append(out, t)
		return nil
	}
	host, workdir := l.effectiveRemote()
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

func readRemoteGrant(root, repoDir string) (*remote.Target, error) {
	if err := refuseTrackedLocal(repoDir); err != nil {
		return nil, err
	}
	l, err := loadLocal(localPath(root))
	if err != nil {
		return nil, err
	}
	host, workdir := l.effectiveRemote()
	if host == "" {
		return nil, nil
	}
	env, err := resolveRemoteEnv(l.MutationRemoteEnv)
	if err != nil {
		return nil, err
	}
	t := &remote.Target{Host: host, Workdir: workdir, Env: env, ScratchBase: l.RemoteScratchBase}
	if err := t.Validate(); err != nil {
		return nil, fmt.Errorf("%s/%s: %w", RootDirName, LocalFile, err)
	}
	return t, nil
}

// resolveRemoteEnv turns the mutation_remote_env NAME allowlist into the
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
func resolveRemoteEnv(allowlist string) ([]remote.EnvVar, error) {
	var out []remote.EnvVar
	for _, name := range strings.Split(allowlist, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		value, ok := os.LookupEnv(name)
		if !ok {
			return nil, fmt.Errorf(
				"%s/%s: mutation_remote_env names %s, but it is not set in this environment.\n\n"+
					"dross stores no values — it reads each allowlisted name from its own environment at\n"+
					"run time. An unset name is refused rather than exported empty, because empty and\n"+
					"absent select different code paths and would change what the run measures.\n\n"+
					"Export it before running, or drop it from `dross local set mutation_remote_env`.",
				RootDirName, LocalFile, name)
		}
		out = append(out, remote.EnvVar{Name: name, Value: value})
	}
	return out, nil
}

// readMutationTuning returns the machine-local worker and test-cpu overrides.
//
// Zero means unset, and unset is not the same as zero: the adapters read it as
// "apply your own default" (NumCPU/2 locally, the probed remote count for a
// remote run). Returning 0 for a value the user actually typed would silently
// discard it, so a value that is present but unusable is an error naming the
// key — the user typed something and deserves to hear that it did not take.
func readMutationTuning(root string) (workers, testCPU int, err error) {
	l, lerr := loadLocal(localPath(root))
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
		return 0, fmt.Errorf("%s/%s: %s = %q is not a number", RootDirName, LocalFile, key, raw)
	}
	if n < 1 {
		return 0, fmt.Errorf("%s/%s: %s = %d must be at least 1", RootDirName, LocalFile, key, n)
	}
	return n, nil
}

func localKeyNames() string {
	names := make([]string, 0, len(localKeys))
	for k := range localKeys {
		names = append(names, k)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

func localPath(root string) string { return filepath.Join(root, LocalFile) }

// loadLocal reads the store. A missing file is an empty store, not an error —
// the file is gitignored, so a fresh clone legitimately has none.
func loadLocal(path string) (*localStore, error) {
	var l localStore
	_, err := toml.DecodeFile(path, &l)
	if errors.Is(err, fs.ErrNotExist) {
		return &localStore{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	return &l, nil
}

// save writes the store, creating it on demand.
func (l *localStore) save(path string) error {
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

// readLocalKey is the reader other commands use. Any failure to read the
// store yields an empty value rather than an error: the store is an
// optimisation for reconciliation, never a gate.
func readLocalKey(root, key string) string {
	l, err := loadLocal(localPath(root))
	if err != nil {
		return ""
	}
	acc, ok := localKeys[key]
	if !ok {
		return ""
	}
	return acc.get(l)
}

func localGet() *cobra.Command {
	return &cobra.Command{
		Use:   "get <key>",
		Short: "Print a machine-local value (empty when unset)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			root, err := FindRoot()
			if err != nil {
				return err
			}
			acc, ok := localKeys[args[0]]
			if !ok {
				return fmt.Errorf("unknown local key %q (want %s)", args[0], localKeyNames())
			}
			l, err := loadLocal(localPath(root))
			if err != nil {
				return err
			}
			// An unset key prints nothing and exits 0 — callers branch on
			// empty output, so a missing value must not look like a failure.
			if v := acc.get(l); v != "" {
				Print(v)
			}
			return nil
		},
	}
}

func localSet() *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Write a machine-local value to .dross/local.toml",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			root, err := FindRoot()
			if err != nil {
				return err
			}
			acc, ok := localKeys[args[0]]
			if !ok {
				return fmt.Errorf("unknown local key %q (want %s)", args[0], localKeyNames())
			}
			path := localPath(root)
			l, err := loadLocal(path)
			if err != nil {
				return err
			}
			acc.set(l, args[1])
			if err := l.save(path); err != nil {
				return err
			}
			Printf("%s = %s\n", args[0], args[1])
			return nil
		},
	}
}
