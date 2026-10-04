package gate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/Rivil/dross/internal/shellscan"
)

// The override store: the gates a human has lifted, until when, and where
// (gate_override). It lives beside gates.toml in ~/.claude/dross/, outside
// every repo, so a repo can never ship a lifted gate. A workflow gate is lifted
// for one repo root; a secret gate — which fires in every directory — is lifted
// machine-wide. Expired entries are ignored; a store that cannot be read lifts
// nothing, and says why to `dross gate status`.

// OverridesFile is the store's name under ~/.claude/dross.
const OverridesFile = "gate-overrides.json"

// Override is one lifted gate.
type Override struct {
	Name string `json:"name"`
	// Repo keys a workflow-gate override to one repo root: the hex sha256 of
	// the root's cleaned absolute path (RepoKey), never the path itself. ""
	// is machine-wide.
	Repo  string    `json:"repo,omitempty"`
	Until time.Time `json:"until"`
}

// OverridesPath is the store under home.
func OverridesPath(home string) string {
	return filepath.Join(home, ".claude", "dross", OverridesFile)
}

// RepoKey is the key a repo root's overrides are filed under.
func RepoKey(root string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(root)))
	return hex.EncodeToString(sum[:])
}

// LoadOverrides reads the store. A missing store is no overrides.
func LoadOverrides(home string) ([]Override, error) {
	p := OverridesPath(home)
	b, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", p, err)
	}
	var list []Override
	if err := json.Unmarshal(b, &list); err != nil {
		return nil, fmt.Errorf("parse %s: %w", p, err)
	}
	return list, nil
}

// SaveOverrides writes the store atomically: a unique temp file renamed over
// it, so a hook reading it mid-write sees the old store or the new one.
func SaveOverrides(home string, list []Override) error {
	p := OverridesPath(home)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), "."+OverridesFile+".*.tmp")
	if err != nil {
		return fmt.Errorf("write %s: %w", p, err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		tmp.Close()
		return fmt.Errorf("write %s: %w", p, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write %s: %w", p, err)
	}
	if err := os.Rename(tmp.Name(), p); err != nil {
		return fmt.Errorf("write %s: %w", p, err)
	}
	return nil
}

// overrideKey is where g's override for root is filed: machine-wide for an
// always-on gate, per repo for a workflow gate.
func overrideKey(g Gate, root string) string {
	if g.Scope == AlwaysOn {
		return ""
	}
	return RepoKey(root)
}

// Lift records g lifted until until — for root when g is workflow-scoped,
// machine-wide when it is always-on — replacing any earlier entry for the
// same gate and scope. Liftable=false gates refuse.
func Lift(home string, g Gate, root string, until time.Time) error {
	if !g.Liftable {
		return fmt.Errorf("gate %s cannot be lifted", g.Name)
	}
	if g.Scope == Workflow && root == "" {
		return fmt.Errorf("gate %s is lifted per repo, and this is not a dross repo", g.Name)
	}
	list, err := LoadOverrides(home)
	if err != nil {
		return err
	}
	key := overrideKey(g, root)
	out := []Override{{Name: g.Name, Repo: key, Until: until.UTC()}}
	for _, o := range list {
		if o.Name != g.Name || o.Repo != key {
			out = append(out, o)
		}
	}
	return SaveOverrides(home, out)
}

// Unlift removes g's override for root (or its machine-wide one).
func Unlift(home string, g Gate, root string) error {
	list, err := LoadOverrides(home)
	if err != nil {
		return err
	}
	key := overrideKey(g, root)
	var out []Override
	for _, o := range list {
		if o.Name != g.Name || o.Repo != key {
			out = append(out, o)
		}
	}
	return SaveOverrides(home, out)
}

// LiftedBy reads the store once and answers Env.Lifted from it at now. A
// store that cannot be read lifts nothing.
func LiftedBy(home string, now time.Time) func(Gate, string) bool {
	list, err := LoadOverrides(home)
	if err != nil {
		list = nil
	}
	return func(g Gate, root string) bool {
		if !g.Liftable {
			return false
		}
		key := overrideKey(g, root)
		for _, o := range list {
			if o.Name == g.Name && o.Repo == key && now.Before(o.Until) {
				return true
			}
		}
		return false
	}
}

// ---- tamper-guard -------------------------------------------------------

// tamper-guard keeps the records the gates trust out of the agent's hands: a
// forged green.json admits any commit, a forged approval.json any edit, and a
// forged override lifts any gate. It refuses file-tool writes to a
// .dross/gate/ record or the override store, and the Bash writes that reach
// them — a redirect, tee, cp or mv. Deletion is refused too: review.json is
// the first record whose absence is MORE lenient (a deleted ledger resets the
// one-fix-round cap and an unavailable verdict), so rm, unlink, mv with a
// record as its source, and the ignored-file sweeps (`git clean -x/-X`,
// `git stash -a`) over .dross/gate/ are refused alongside the writes. Reads
// pass. Like gate-off-guard it cannot be lifted, and it fires everywhere: a
// .dross/gate/ is the same record whichever repo it sits in.
func init() {
	Register(Gate{
		Name: "tamper-guard", Scope: AlwaysOn, Liftable: false,
		Claims: func(c *Call) bool { return tamperTarget(c) != "" },
		Judge: func(c *Call) (*Refusal, error) {
			return NewRefusal(
				fmt.Sprintf("%s is a record the tool gates trust; writing or deleting it from a tool call would forge a green, an approval, a review or an override", tamperTarget(c)),
				"let dross write it — `dross test` records a green, an AskUserQuestion approval records an approval, and only a human lifts a gate, from their own terminal")
		},
	})
}

// isGateRecord reports whether p (absolute, or relative to nothing) is under a
// .dross/gate/ directory or is the override store.
func isGateRecord(p, home string) bool {
	clean := path.Clean(filepath.ToSlash(p))
	if strings.Contains("/"+clean+"/", "/.dross/gate/") {
		return true
	}
	return home != "" && filepath.Clean(p) == OverridesPath(home)
}

// tamperTarget names the gate record the call would write, or "".
func tamperTarget(c *Call) string {
	switch c.ToolName {
	case "Edit", "MultiEdit", "Write", "NotebookEdit":
		if f := c.FilePath(); f != "" && isGateRecord(f, c.Home) {
			return f
		}
		return ""
	case "Bash":
	default:
		return ""
	}
	for _, cmd := range c.Script().Commands {
		var targets []string
		targets = append(targets, cmd.Outputs()...)
		args := cmd.Args()
		var operands []string
		for _, a := range args {
			if !strings.HasPrefix(a, "-") {
				operands = append(operands, a)
			}
		}
		removes := false
		switch cmd.Name() {
		case "tee":
			targets = append(targets, operands...)
		case "cp":
			if len(operands) > 1 {
				targets = append(targets, operands[len(operands)-1])
			}
		case "mv":
			// Every operand: the target is written, each source is deleted.
			targets = append(targets, operands...)
			removes = true
		case "rm", "unlink":
			targets = append(targets, operands...)
			removes = true
		case "git":
			if g, ok := cmd.Git(); ok {
				if p := ignoredSweep(c, g); p != "" {
					return p
				}
			}
		}
		for _, t := range targets {
			p := cmd.Resolve(t)
			if isGateRecord(p, c.Home) {
				return p
			}
			// Removing .dross/ itself takes .dross/gate/ with it.
			if removes && filepath.Base(p) == ".dross" {
				return filepath.Join(p, "gate")
			}
		}
	}
	return ""
}

// ignoredSweep names the .dross/gate/ a git command would delete as ignored
// files — `git clean` with -x or -X, `git stash` with -a/--all — when one of
// its pathspecs (none means the working directory) reaches the repo's gate
// directory. A pathspec excluding .dross/ keeps the sweep clear of it.
func ignoredSweep(c *Call, g shellscan.GitCall) string {
	sweeps := false
	var specs []string
	afterDD := false
	for _, a := range g.Args {
		short := strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--")
		switch {
		case afterDD:
			specs = append(specs, a)
		case a == "--":
			afterDD = true
		case g.Sub == "clean" && short && strings.ContainsAny(a[1:], "xX"):
			sweeps = true
		case g.Sub == "stash" && (a == "--all" || (short && strings.Contains(a[1:], "a"))):
			sweeps = true
		case g.Sub == "clean" && !strings.HasPrefix(a, "-"):
			specs = append(specs, a)
		}
	}
	if !sweeps {
		return ""
	}
	root, err := c.Root()
	if err != nil || root == "" {
		return ""
	}
	gateDir := filepath.Join(root, ".dross", "gate")
	if len(specs) == 0 {
		specs = []string{"."}
	}
	for _, s := range specs {
		if strings.HasPrefix(s, ":") && strings.Contains(s, "exclude") && strings.Contains(s, ".dross") {
			return ""
		}
	}
	at := shellscan.Command{Dir: g.Dir}
	for _, s := range specs {
		if strings.HasPrefix(s, ":") {
			continue
		}
		p := at.Resolve(s)
		if p == gateDir || strings.HasPrefix(p+string(filepath.Separator), gateDir+string(filepath.Separator)) ||
			strings.HasPrefix(gateDir, p+string(filepath.Separator)) {
			return gateDir
		}
	}
	return ""
}
