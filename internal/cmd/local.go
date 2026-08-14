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

	// MutationRemoteHost and MutationRemoteWorkdir authorize a mutation run to
	// execute on another machine.
	//
	// Both are deliberately ABSENT from localKeys, on exactly the
	// TrustedTestCommand precedent above and for a strictly larger reason:
	// configuring a remote is code execution on a machine of the config's
	// choosing. `dross mutation remote grant` is the only writer, and it prints
	// the host and workdir it is about to authorize BEFORE it writes them. A
	// generic key-writer would let an agent grant that on the user's behalf
	// without ever showing them what for.
	//
	// They live here rather than in project.toml for the same reason
	// allow_hosts does: a committed remote host would be self-authorizing, and
	// project.Load refuses one by name (see the trap fields there).
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
	// mutation_remote_host and mutation_remote_workdir are NOT here. See the
	// struct fields — they are granted by `dross mutation remote grant`, which
	// shows the user what it is authorizing, and by nothing else.
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
func readRemoteGrant(root, repoDir string) (*remote.Target, error) {
	if err := refuseTrackedLocal(repoDir); err != nil {
		return nil, err
	}
	l, err := loadLocal(localPath(root))
	if err != nil {
		return nil, err
	}
	if l.MutationRemoteHost == "" {
		return nil, nil
	}
	t := &remote.Target{Host: l.MutationRemoteHost, Workdir: l.MutationRemoteWorkdir}
	if err := t.Validate(); err != nil {
		return nil, fmt.Errorf("%s/%s: %w", RootDirName, LocalFile, err)
	}
	return t, nil
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
