package mutation

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Rivil/dross/internal/argfence"
	"github.com/Rivil/dross/internal/remote"
)

// Stryker adapter for TS/JS/Svelte mutation testing via stryker-mutator.
//
// Stryker writes its report to <project>/reports/mutation/mutation.json
// in the mutation-testing-report-schema format. We invoke stryker via
// the project's runtime (docker or native), then parse the report.
type Stryker struct {
	// Prefix is the runtime command prefix (e.g. "docker compose exec app").
	// Empty means run natively in cwd.
	Prefix string
	// ProjectRoot is the absolute path the runtime sees as the project root.
	// For native, this is the cwd. For docker, this is the cwd on the host
	// (we still read reports from the host filesystem).
	ProjectRoot string
	// Workdir is the repo-relative package that hosts stryker and its config
	// in a monorepo (e.g. "web" when vitest + stryker.config.json live in
	// web/, not the repo root). Empty means the repo root. The adapter runs
	// stryker there, strips the prefix from --mutate paths, and re-prefixes
	// report paths so tests.json stays repo-relative.
	Workdir string

	// Remote delegates the run to another machine. Nil runs locally, and a
	// local run is byte-identical to what it was before remoting existed.
	//
	// A NAMED field rather than an embedded Launcher — see launcher.go.
	Remote *remote.Target
	// CacheVars are the environment variable names this stack's toolchain reads
	// for its build cache (stack profile mutation_cache.vars). A run is pointed
	// at a scratch copy and the scratch is wiped when the run ends; empty leaves
	// the run exactly as it was.
	CacheVars []string

	// PackageManager is the project's own package manager
	// (stack.package_manager), and is only consulted for a REMOTE run: it keys
	// the lockfile-respecting install that has to happen on the host before
	// stryker runs there. Unset is an error rather than a guess at npm.
	PackageManager string
}

func (s *Stryker) Name() string { return "stryker" }

func (s *Stryker) Supports(file string) bool {
	switch strings.ToLower(filepath.Ext(file)) {
	case ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs", ".svelte":
		return true
	}
	return false
}

// Run invokes stryker on the given files, then parses the JSON report.
func (s *Stryker) Run(files []string) (*Report, error) {
	if len(files) == 0 {
		return &Report{Tool: s.Name()}, nil
	}

	// Built before anything is spawned, and carrying Workdir: cmd.Dir on a
	// LOCAL ssh process says nothing about the remote cwd, so the monorepo knob
	// has to reach the remote as part of the command itself or stryker runs in
	// the wrong package.
	lr, err := newLauncher(s.Name(), s.Prefix, s.Remote, s.ProjectRoot, s.Workdir, s.CacheVars)
	if err != nil {
		return nil, err
	}

	defer func() { _ = lr.Close() }()
	lr.PackageManager = s.PackageManager
	// Resolved BEFORE anything is spawned. An unset or unrecognised package
	// manager is a refusal the user can act on; discovering it after the tree
	// has been pushed is the same refusal with a wasted rsync in front of it.
	if _, err := lr.restoreArgv(); err != nil {
		return nil, err
	}

	// The trimmed, UNESCAPED request list — the form the report's file keys
	// come back in, which is what checkInstrumented diffs against below. The
	// argv carries the ESCAPED form; comparing THAT to report keys would find
	// every bracket path "missing".
	args, requested, err := s.runArgs(files)
	if err != nil {
		return nil, err
	}
	reportPath := s.reportPath()
	if err := lr.clearReport("", reportPath); err != nil {
		return nil, err
	}
	cmd, err := lr.toolCmd(args, func(a []string) *exec.Cmd { return strykerBuildCmd(s, a) })
	if err != nil {
		return nil, err
	}
	if !lr.remoteRun() {
		// Only meaningful locally. Remotely the workdir is the `cd` inside the
		// ssh command, and setting it here would chdir the ssh client instead.
		cmd.Dir = s.workDir()
	}
	// TEED, not captured. The user still sees the whole stream as it arrives;
	// a bounded prefix is retained purely so a failure can quote the real cause
	// instead of guessing at it.
	head := &headBuffer{limit: strykerHeadBytes}
	sink := io.MultiWriter(os.Stderr, head)
	cmd.Stdout = sink
	cmd.Stderr = sink
	if err := cmd.Run(); err != nil {
		// Stryker exits non-zero when surviving mutants exist —
		// that's a successful run with bad results, not an adapter
		// failure. We still try to read the report.
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			return nil, fmt.Errorf("stryker invocation failed: %w (is stryker installed in the project? `npm i -D %s` or equivalent)", err, strykerPin)
		}
	}

	if err := lr.fetchReport("", reportPath); err != nil {
		fmt.Fprintf(os.Stderr, "stryker: fetch report: %v\n", err)
	}

	b, err := os.ReadFile(reportPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			// NOT "check stryker config". That advice was wrong in every case
			// it was actually hit: a dry run killed by a missing environment
			// variable, a --mutate list that resolved to nothing, a crash in
			// the instrumenter. The config was fine each time and the real
			// cause was sitting at the HEAD of the output, which the user had
			// just watched scroll past. Quote it.
			return nil, fmt.Errorf("stryker did not write a report at %s.\n%s", reportPath, head.quote(strykerHeadLines))
		}
		return nil, fmt.Errorf("read stryker report: %w", err)
	}
	report, err := ParseStrykerJSON(b)
	if err != nil {
		return nil, err
	}
	// BEFORE rePrefixFiles, which rewrites the report's keys from
	// workdir-relative to repo-relative. `requested` is the trimmed
	// workdir-relative form, so the two only speak the same paths on this side
	// of that call — one line later and every path would look dropped.
	if err := s.checkInstrumented(b, requested, head); err != nil {
		return nil, err
	}
	s.rePrefixFiles(report)
	return report, nil
}

const (
	// How much of the tool's output to retain, and how much of THAT to quote.
	// Bounded so a runaway run cannot be held in memory, generous enough that
	// a stack trace preceded by a banner still contains the cause.
	strykerHeadBytes = 64 << 10
	strykerHeadLines = 40
)

// headBuffer retains the first `limit` bytes written through it and silently
// discards the rest, always reporting a full write so it can sit inside an
// io.MultiWriter without truncating the stream its sibling is rendering.
type headBuffer struct {
	limit int
	buf   bytes.Buffer
}

func (h *headBuffer) Write(p []byte) (int, error) {
	if room := h.limit - h.buf.Len(); room > 0 {
		if len(p) <= room {
			h.buf.Write(p)
		} else {
			h.buf.Write(p[:room])
		}
	}
	// len(p), never the amount kept: a short count is an io.ErrShortWrite to
	// io.MultiWriter, which would abort the write to os.Stderr as well and
	// truncate the live output the moment the cap was reached.
	return len(p), nil
}

// quote renders at most n lines of the retained head, indented, for embedding
// in an error.
func (h *headBuffer) quote(n int) string {
	text := strings.TrimRight(h.buf.String(), "\n")
	if text == "" {
		return "stryker produced no output at all — it may not have started."
	}
	lines := strings.Split(text, "\n")
	truncated := false
	if len(lines) > n {
		lines, truncated = lines[:n], true
	}
	var b strings.Builder
	b.WriteString("the head of stryker's output, which is where the cause is:\n\n")
	for _, l := range lines {
		b.WriteString("    ")
		b.WriteString(l)
		b.WriteString("\n")
	}
	if truncated {
		b.WriteString("    … (output continues above)\n")
	}
	return b.String()
}

// strykerDropWarningText is Stryker's own wording when a --mutate glob resolves
// to no file (@stryker-mutator/core, src/fs/project-reader.ts). Named here so
// the constant and the error message that surfaces it cannot drift apart.
const strykerDropWarningText = "did not result in any files"

// checkInstrumented hard-fails the run when stryker did not instrument a file
// it was asked to mutate (locked decision drop_behaviour).
//
// A PARTIAL SCORE IS WORSE THAN NO SCORE, because it looks complete. That is
// not hypothetical: on 2026-08-26 six SvelteKit bracket route paths were
// dropped from a --mutate list by a glob mismatch, and the run reported a
// perfectly ordinary score over the files that survived the drop. Nobody
// noticed, because nothing said anything.
//
// THE SIGNAL IS THE SET OF PATHS THE REPORT MENTIONS, not the set carrying
// counted mutants, and that distinction is load-bearing. A file whose glob
// matched but whose every mutant is Ignored — the `// Stryker disable all`
// case — appears in the report's `files` object with a mutants array, yet
// ParseStrykerJSON adds NO row for it (Ignored is not counted). Diffing
// against report.Files would therefore hard-fail on a file that was
// instrumented perfectly well, and such files are common enough to make the
// check unusable. Reading the raw keys tells the two apart exactly.
func (s *Stryker) checkInstrumented(data []byte, requested []string, head *headBuffer) error {
	var raw strykerReport
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("decode stryker report: %w", err)
	}
	mentioned := make(map[string]struct{}, len(raw.Files))
	for path := range raw.Files {
		mentioned[filepath.ToSlash(path)] = struct{}{}
	}

	var dropped []string
	for _, want := range requested {
		want = filepath.ToSlash(want)
		if _, ok := mentioned[want]; ok {
			continue
		}
		// Suffix fallback: a report keyed on absolute paths still names the
		// same file. A genuinely dropped path appears in no key at all, by
		// suffix or otherwise, so this cannot mask the fault it is checking.
		matched := false
		for got := range mentioned {
			if strings.HasSuffix(got, "/"+want) {
				matched = true
				break
			}
		}
		if !matched {
			dropped = append(dropped, want)
		}
	}
	if len(dropped) == 0 {
		return nil
	}

	msg := fmt.Sprintf(
		"stryker instrumented none of %d file(s) it was asked to mutate: %s.\n"+
			"Refusing to report a score over the rest: a partial run looks exactly like a complete one,\n"+
			"which is how six route files vanished from a run unnoticed on 2026-08-26.\n",
		len(dropped), strings.Join(dropped, ", "))
	if strings.Contains(head.buf.String(), strykerDropWarningText) {
		msg += "stryker said so itself — look for \"" + strykerDropWarningText + "\" below.\n"
	}
	return fmt.Errorf("%s\n%s", msg, head.quote(strykerHeadLines))
}

// strykerPin is the exact @stryker-mutator/core version dross invokes.
//
// `npx --yes @stryker-mutator/core` with no version is a supply-chain hole, not
// a convenience: --yes suppresses the install prompt, so an unpinned spec means
// dross silently downloads and executes whatever the registry serves as latest
// at that moment — on a developer machine, inside the repo, with the repo's own
// test command wired in. That is the shape the 2025–2026 npm registry
// compromises took. A pinned version narrows it to one artifact that can be
// reviewed before it changes, and bumping it becomes a visible diff.
//
// Bump deliberately: read the release notes, then update this one constant.
// Both the invocation and the install hint read it, and TestStrykerHintUsesSamePin
// fails if they ever drift apart.
const strykerPin = "@stryker-mutator/core@9.6.1"

// runArgs builds the stryker invocation. The scoped package name matters:
// bare "stryker" on the npm registry is the ancient pre-scoped package and
// crashes on modern Node (MODULE_NOT_FOUND); npx resolves a project-local
// @stryker-mutator/core first and --yes fetches the right fallback.
// --mutate paths are workdir-relative because stryker runs in Workdir.
//
// npx has no end-of-options token — it consumes leading-dash arguments itself
// before the wrapped binary ever sees them — so a derived value beginning with
// a dash is refused rather than fenced. Both derived inputs are checked: the
// Workdir, which comes straight from project.toml, and each --mutate entry
// AFTER the prefix trim, because it is the trimmed form that lands in the argv.
//
// It returns the argv AND the trimmed, UNESCAPED request list. The second
// value is what the report's file keys can be compared against: the keys come
// back in the real path form, so a caller diffing them against the argv's
// escaped form would find every bracket path "missing".
//
// ORDER IS LOAD-BEARING. The escape runs last — after the workdir trim and
// after the argfence check, never instead of either. Escaping first would
// leave the fence inspecting a string the argv no longer contains.
func (s *Stryker) runArgs(files []string) (argv []string, requested []string, err error) {
	if _, err := argfence.Fence("npx", "workdir", s.Workdir); err != nil {
		return nil, nil, err
	}
	requested = make([]string, 0, len(files))
	for _, f := range files {
		if s.Workdir != "" {
			f = strings.TrimPrefix(f, s.Workdir+"/")
		}
		requested = append(requested, f)
	}
	if _, err := argfence.Fence("npx", "mutate path", requested...); err != nil {
		return nil, nil, err
	}
	mutate := make([]string, 0, len(requested))
	for _, f := range requested {
		mutate = append(mutate, escapeGlobMeta(f))
	}
	return []string{"npx", "--yes", strykerPin, "run",
		"--mutate", strings.Join(mutate, ","),
		"--reporters", "json"}, requested, nil
}

// escapeGlobMeta makes a literal path safe to hand Stryker as a --mutate glob.
//
// TWO layers rewrite this string on its way to the tool, and a fix for either
// one alone does nothing. Both were measured against the installed toolchain on
// 2026-08-26 rather than reasoned about.
//
// LAYER 1 — minimatch, inside Stryker. "[" opens a character class, so
// src/routes/recipes/[id]/+page.server.ts is read as "one of i, d" and matches
// nothing on disk. Stryker then drops the file from the run WITHOUT an error:
// it logs "did not result in any files" and reports a score over whatever
// remained, which looks exactly like a healthy run. Six real SvelteKit route
// files vanished that way on 2026-08-26.
//
// The bracket-EXPRESSION form is the fix: "[[]" is a class containing only "[",
// "[]]" one containing only "]". Backslash escaping is the obvious alternative
// and it is WRONG — Stryker builds its FileMatcher with
// normalizeFileName(path.resolve(pattern)) and normalizeFileName is
// replace(/\\/g, "/"), so every backslash becomes a forward slash before
// minimatch sees the pattern and "\\[id\\]" matches nothing either, silently.
//
// LAYER 2 — npm, before Stryker is even spawned. dross invokes stryker through
// `npx`, and npm runs the command via `sh -c` after escaping each argument with
// @npmcli/promise-spawn's sh(), which quotes only when the argument matches:
//
//	/[\t\n\r "#$&'()*;<>?\\`|~]/
//
// "[" and "]" are absent from that set. So a bracket-only path is handed to the
// shell UNQUOTED, the shell glob-expands "[[]id[]]" back to the real on-disk
// name "[id]", and Stryker receives exactly the raw form layer 1 was escaping
// away. The cruelty is the condition: the shell only expands a glob that
// MATCHES, so the escape is undone precisely when the file exists — the one
// case that matters. (npm 11.14.1; "*" and "?" ARE in the set and pass through
// untouched, which is why this shows up as a brackets-only defect.)
//
// So the last path segment is wrapped in an extglob, "@(name)". To minimatch
// that is a no-op — "@(x)" matches exactly x — while the parentheses put the
// argument inside npm's quoting set, so the shell never sees a glob at all.
// Measured: the wrapped form reaches Stryker byte-identical, and matches the
// target file while not matching an unrelated one.
//
// SINGLE PASS, deliberately. Two sequential ReplaceAll calls corrupt nested
// brackets — the second pass rewrites the "]" the first pass just emitted, so
// "[[opt]]" comes out mangled instead of "[[][[]opt[]][]]".
//
// A bracket-free path is returned byte-identical: neither layer misbehaves on
// it, and rewriting it would change every existing measurement.
func escapeGlobMeta(path string) string {
	if !strings.ContainsAny(path, "[]") {
		return path
	}
	var b strings.Builder
	b.Grow(len(path) + 8)
	for _, r := range path {
		switch r {
		case '[':
			b.WriteString("[[]")
		case ']':
			b.WriteString("[]]")
		default:
			b.WriteRune(r)
		}
	}
	escaped := b.String()

	// Wrap the FINAL segment only. An extglob is matched within one path
	// segment, so a wrapper spanning "/" would stop matching.
	cut := strings.LastIndex(escaped, "/")
	return escaped[:cut+1] + "@(" + escaped[cut+1:] + ")"
}

// strykerBuildCmd is the process-construction seam — see gremlinsBuildCmd.
var strykerBuildCmd = (*Stryker).buildCmd

// workDir is the directory stryker runs in — ProjectRoot, or the monorepo
// package under it.
func (s *Stryker) workDir() string {
	if s.Workdir == "" {
		return s.ProjectRoot
	}
	return filepath.Join(s.ProjectRoot, s.Workdir)
}

// reportPath is where stryker's json reporter writes, under the workdir.
func (s *Stryker) reportPath() string {
	return filepath.Join(s.workDir(), "reports", "mutation", "mutation.json")
}

// rePrefixFiles restores repo-relative paths in a report parsed from a
// workdir run, so tests.json speaks the same paths as changes.json — and so
// diff scoping matches the same paths in the per-file rows it scores from.
// Both surfaces must move together: a Surviving entry the scope matches whose
// row key it doesn't would score the mutant out while still reporting it.
func (s *Stryker) rePrefixFiles(r *Report) {
	if s.Workdir == "" {
		return
	}
	for i := range r.Surviving {
		r.Surviving[i].File = s.Workdir + "/" + r.Surviving[i].File
	}
	if len(r.Files) == 0 {
		return
	}
	files := make(map[string]FileStat, len(r.Files))
	for name, st := range r.Files {
		k := s.Workdir + "/" + name
		files[k] = files[k].plus(st)
	}
	r.Files = files
}

// buildCmd returns an exec.Cmd that respects s.Prefix.
// If Prefix is empty, runs args natively. Otherwise prepends Prefix
// (split on whitespace) to args.
func (s *Stryker) buildCmd(args []string) *exec.Cmd {
	if s.Prefix == "" {
		return exec.Command(args[0], args[1:]...)
	}
	prefix := strings.Fields(s.Prefix)
	full := append(prefix, args...)
	return exec.Command(full[0], full[1:]...)
}

// strykerReport mirrors the subset of the Stryker JSON schema we care about.
// Schema: github.com/stryker-mutator/mutation-testing-elements
// (mutation-testing-report-schema)
type strykerReport struct {
	SchemaVersion string                 `json:"schemaVersion"`
	Files         map[string]strykerFile `json:"files"`
}

type strykerFile struct {
	Language string          `json:"language"`
	Source   string          `json:"source"`
	Mutants  []strykerMutant `json:"mutants"`
}

type strykerMutant struct {
	ID           string          `json:"id"`
	MutatorName  string          `json:"mutatorName"`
	Replacement  string          `json:"replacement"`
	Status       string          `json:"status"`
	StatusReason string          `json:"statusReason,omitempty"`
	Location     strykerLocation `json:"location"`
	Description  string          `json:"description,omitempty"`
}

type strykerLocation struct {
	Start strykerPos `json:"start"`
	End   strykerPos `json:"end"`
}

type strykerPos struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

// ParseStrykerJSON converts a Stryker mutation.json payload into the
// normalised Report shape verify uses.
//
// Status mapping (Stryker → Report):
//
//	Killed                  → killed
//	Survived                → survived (recorded with snippet)
//	Timeout                 → timeout
//	RuntimeError, CompileError → errors
//	NoCoverage              → survived (test never even ran the mutant)
//	Pending, Ignored        → ignored (not counted)
//
// Score uses Stryker's convention: killed / (killed + survived + timeout)
// — the "mutation score" excluding errors and ignored mutants.
func ParseStrykerJSON(data []byte) (*Report, error) {
	var raw strykerReport
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("decode stryker report: %w", err)
	}
	r := &Report{Tool: "stryker"}
	for path, f := range raw.Files {
		for _, m := range f.Mutants {
			switch m.Status {
			case "Killed":
				r.Killed++
				r.addFile(path, FileStat{Killed: 1})
			case "Survived", "NoCoverage":
				r.Survived++
				r.addFile(path, FileStat{Survived: 1})
				r.Surviving = append(r.Surviving, Mutant{
					File:    path,
					Line:    m.Location.Start.Line,
					Op:      m.MutatorName,
					Snippet: m.Replacement,
				})
			case "Timeout":
				r.Timeout++
				r.addFile(path, FileStat{Timeout: 1})
			case "RuntimeError", "CompileError":
				r.Errors++
				r.addFile(path, FileStat{Errors: 1})
			case "Pending", "Ignored", "":
				// not counted
			default:
				// future statuses — count as errors so they're visible
				r.Errors++
				r.addFile(path, FileStat{Errors: 1})
			}
		}
	}
	r.Score = PooledScore(r.Killed, r.Survived, r.Timeout)
	return r, nil
}
