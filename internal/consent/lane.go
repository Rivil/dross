package consent

// Per-lane consent: one grant per [[runtime.test_lane]] for its command (and
// prepare, selector scoping, vouch and relate), and a second, independent grant
// for its install line. Both ride the same Store the whole-suite grant does.

import (
	"fmt"
	"strings"

	"github.com/Rivil/dross/internal/project"
)

// LaneFrame is the domain separator that keeps a prepared lane's consent line
// out of the namespace a bare command occupies.
//
// A NUL cannot appear in an argv element, so no command line a user could
// actually run can spell this prefix — which is what makes the "no pair can
// forge a no-prepare lane's fingerprint" claim structural rather than
// probabilistic.
const LaneFrame = "dross:lane-prepare:v1\x00"

// LaneTemplateFrame is the THIRD namespace, for a lane that declares selector
// scoping of its own.
//
// It is disjoint from LaneFrame and from the unframed namespace by
// construction, not by being hard to hit: no unframed line may carry a NUL, and
// the two framed prefixes differ in their own text, so no byte string can be
// read as belonging to two of the three. That is what keeps a templated lane
// from running under a grant issued for a prepared one carrying the same two
// command lines.
const LaneTemplateFrame = "dross:lane-template:v1\x00"

// LaneVouchFrame is the FOURTH namespace, for a lane trusted to admit a commit
// on a scoped green.
//
// Disjoint from the other three the way LaneTemplateFrame is, and for a sharper
// reason: a lane that vouches can put a commit past the gate, which a lane that
// only runs tests cannot. No grant issued for the same lines before the lane
// could vouch may carry over to it.
const LaneVouchFrame = "dross:lane-vouch:v1\x00"

// LaneLine returns the exact byte string ONE lane's consent grant is
// taken over — the value Fingerprint hashes and the store records.
//
// Five properties, each load-bearing:
//
//   - A lane declaring NO prepare returns its command UNCHANGED. Framing
//     applied unconditionally would re-fingerprint every lane grant already
//     written on every machine, staling them all over a document nobody
//     edited.
//   - A prepared lane's two lines are LENGTH-FRAMED, not concatenated. Naive
//     concatenation collides {prepare:"a", command:"bc"} with {prepare:"ab",
//     command:"c"} — a lane re-split across its two fields keeping a grant
//     issued for neither.
//   - The framed form is NUL-delimited and no unframed line may carry a NUL,
//     so feeding those bytes back as a bare command misses rather than forging
//     the pair's own fingerprint. The namespaces are disjoint by construction,
//     not by being hard to hit.
//   - A lane declaring selector scoping of its own takes a THIRD form under
//     LaneTemplateFrame, binding selector_template and selector_join alongside
//     the two command lines. Adding or changing either therefore stales the
//     grant rather than letting an unread line run under one issued before it —
//     the template is arbitrary user text on the spawned line, fenced by
//     consent exactly as command and prepare are.
//   - A vouching lane takes a FOURTH form under LaneVouchFrame, binding relate
//     alongside every line the template form binds. Turning vouch on or changing
//     relate therefore stales the grant: the first widens what the lane may do,
//     and the second rewrites what decides which paths count as covered. A lane
//     with vouch off keeps its form whatever its relate says, because nothing
//     runs relate there, so no grant already written is disturbed.
//
// A lane with no command returns the empty string whatever its prepare says:
// consent binds to something runnable, and LaneConsented's NotApplicable
// arm is keyed on exactly that emptiness.
func LaneLine(lane project.TestLane) string {
	if strings.TrimSpace(lane.Command) == "" {
		return ""
	}
	// A NUL is what makes the frame unforgeable, and it only works if no
	// UNFRAMED line can carry one: otherwise a lane declaring no prepare and a
	// command spelled exactly like the frame would hash the frame itself, and
	// the grant would transfer between two lanes sharing no line at all.
	//
	// Refusing costs nothing real. An argv element is NUL-terminated, so a
	// command containing one cannot be exec'd under any shell — the lane is
	// unrunnable, and binding consent to it would be binding to something that
	// can never run. Empty here means exactly that, and LaneConsented turns it
	// into the same NotApplicable a commandless lane gets.
	if strings.ContainsRune(lane.Command, 0) || strings.ContainsRune(lane.Prepare, 0) ||
		strings.ContainsRune(lane.SelectorTemplate, 0) || strings.ContainsRune(lane.SelectorJoin, 0) ||
		strings.ContainsRune(lane.Relate, 0) {
		return ""
	}
	// Ahead of the template form, not beside it: a vouching lane may declare
	// selector scoping too, and the frame says it vouches whichever lines it has.
	if lane.Vouch {
		return fmt.Sprintf("%s%d\x00%s\x00%d\x00%s\x00%d\x00%s\x00%d\x00%s\x00%d\x00%s",
			LaneVouchFrame,
			len(lane.Prepare), lane.Prepare,
			len(lane.Command), lane.Command,
			len(lane.SelectorTemplate), lane.SelectorTemplate,
			len(lane.SelectorJoin), lane.SelectorJoin,
			len(lane.Relate), lane.Relate)
	}
	// Gated on either scoping field being declared, not on the template alone:
	// a join carries user text into the run just as a template does, so a lane
	// hand-edited to carry one must not ride a grant taken before it. Neither
	// field existed before this phase, so no grant already written anywhere is
	// disturbed by framing that shape.
	if lane.SelectorTemplate != "" || lane.SelectorJoin != "" {
		return fmt.Sprintf("%s%d\x00%s\x00%d\x00%s\x00%d\x00%s\x00%d\x00%s",
			LaneTemplateFrame,
			len(lane.Prepare), lane.Prepare,
			len(lane.Command), lane.Command,
			len(lane.SelectorTemplate), lane.SelectorTemplate,
			len(lane.SelectorJoin), lane.SelectorJoin)
	}
	if lane.Prepare == "" {
		return lane.Command
	}
	return fmt.Sprintf("%s%d\x00%s\x00%d\x00%s", LaneFrame, len(lane.Prepare), lane.Prepare, len(lane.Command), lane.Command)
}

// LaneConsented reports what this machine has said about ONE lane's command.
//
// It returns the same State ladder CheckConsent does, and for the same
// reason: "you have never trusted this lane" and "this lane's command has
// changed since you trusted it" are different situations calling for different
// reactions, and collapsing them reports a rewritten command as a routine first
// run. The states are per lane, so a repo where one lane went stale still has
// four granted lanes and the refusal says which one to look at.
//
// repoDir travels alongside root, exactly as it does for CheckConsent, so the
// tracked-store refusal is shared rather than restated: a committed local.toml
// is a repo authorizing its own lane commands, and it is refused UNREAD.
func LaneConsented(store Store, repoDir, name, line string) (State, error) {
	if err := RefuseTrackedLocal(repoDir); err != nil {
		return Refused, err
	}
	if strings.TrimSpace(line) == "" {
		return NotApplicable, fmt.Errorf("%w: %s", ErrNoLaneCommand, name)
	}
	l, err := store.Load()
	if err != nil {
		// An unparseable store is not consent. Fail closed, and say why.
		return Absent, fmt.Errorf("%w: %v", ErrNoConsent, err)
	}
	got, ok := l.TrustedLaneCommands[name]
	if !ok || got == "" {
		return Absent, ErrNoConsent
	}
	if got != Fingerprint(line) {
		return Stale, ErrStaleConsent
	}
	return Granted, nil
}

// GrantLaneConsent records consent for one lane, leaving every other lane's
// grant exactly as it was.
//
// Per lane is the whole point of the map (see Grants.TrustedLaneCommands):
// a grant that replaced the store would make trusting the docs lane revoke the
// Go lane, so the user would re-consent to everything every time they touched
// anything.
func GrantLaneConsent(store Store, name, line string) error {
	l, err := store.Load()
	if err != nil {
		return err
	}
	if l.TrustedLaneCommands == nil {
		l.TrustedLaneCommands = map[string]string{}
	}
	l.TrustedLaneCommands[name] = Fingerprint(line)
	return store.Save(l)
}

// RevokeLaneConsent drops one lane's grant. Removing a lane calls it, so a
// name that is later re-added starts ungranted rather than inheriting a
// fingerprint issued for whatever the deleted lane used to run.
//
// A missing entry is not an error: the caller is expressing "this lane has no
// grant", and that is already true.
func RevokeLaneConsent(store Store, name string) error {
	l, err := store.Load()
	if err != nil {
		return err
	}
	// BOTH grants, at this one site. A lane's name keys two stores now, and a
	// removal that dropped only the command grant would leave an install grant
	// behind under a name nothing declares — until someone re-added a lane
	// under it, which would then start authorized to install whatever the
	// deleted lane's line said.
	//
	// Checked as a pair rather than short-circuiting on the command grant: a
	// lane may hold an install grant and no command grant at all, and an
	// early return on the command map would skip it.
	_, hadCommand := l.TrustedLaneCommands[name]
	_, hadInstall := l.TrustedLaneInstalls[name]
	if !hadCommand && !hadInstall {
		return nil
	}
	delete(l.TrustedLaneCommands, name)
	delete(l.TrustedLaneInstalls, name)
	if len(l.TrustedLaneCommands) == 0 {
		// Back to nil so omitempty keeps an empty table out of the file —
		// a bare [trusted_lane_commands] header reads as a store that holds
		// something.
		l.TrustedLaneCommands = nil
	}
	if len(l.TrustedLaneInstalls) == 0 {
		l.TrustedLaneInstalls = nil
	}
	return store.Save(l)
}

// --- per-lane INSTALL consent ---

// LaneInstallFrame is the domain separator for an install grant.
//
// Its own frame, disjoint from LaneFrame and from the unframed namespace a bare
// command occupies, so no command line a user could actually run can spell it —
// a NUL cannot appear in an argv element. The grants live in separate maps, so
// this is not the only thing keeping them apart; it is what makes "a command
// cannot forge an install grant" structural rather than a property of how the
// store happens to be keyed today.
const LaneInstallFrame = "dross:lane-install:v1\x00"

// LaneInstallLine returns the exact byte string ONE lane's INSTALL grant
// is taken over.
//
// Length-framed like LaneLine's prepared form rather than bare, so the
// frame stays unambiguous if a second field is ever bound into it — a naive
// join is what lets two fields re-split and keep a grant issued for neither.
//
// Empty means un-grantable, and there are two ways to be: a lane declaring no
// install line has nothing to consent to, and a line carrying a NUL can never
// be exec'd under any shell, so binding consent to it would bind to something
// that can never run. LaneInstallConsented turns both into
// NotApplicable.
func LaneInstallLine(lane project.TestLane) string {
	if strings.TrimSpace(lane.Install) == "" {
		return ""
	}
	if strings.ContainsRune(lane.Install, 0) {
		return ""
	}
	return fmt.Sprintf("%s%d\x00%s", LaneInstallFrame, len(lane.Install), lane.Install)
}

// LaneInstallConsented reports what this machine has said about ONE lane's
// install line.
//
// It answers INDEPENDENTLY of LaneConsented, which is the whole point of the
// separate store (locked install_consent): adding an install line to a lane
// that already runs green must leave its test grant reading Granted while this
// reports Absent, so one edit yields two answers rather than one refusal of
// something that never changed.
//
// Same ladder, same tracked-store refusal, for the reasons LaneConsented has
// them: a rewritten install line and a never-trusted one need different
// reactions, and a committed local.toml is a repo authorizing its own install
// commands.
func LaneInstallConsented(store Store, repoDir, name, line string) (State, error) {
	if err := RefuseTrackedLocal(repoDir); err != nil {
		return Refused, err
	}
	if strings.TrimSpace(line) == "" {
		return NotApplicable, fmt.Errorf("%w: %s", ErrNoLaneInstall, name)
	}
	l, err := store.Load()
	if err != nil {
		// An unparseable store is not consent. Fail closed, and say why.
		return Absent, fmt.Errorf("%w: %v", ErrNoConsent, err)
	}
	got, ok := l.TrustedLaneInstalls[name]
	if !ok || got == "" {
		return Absent, ErrNoConsent
	}
	if got != Fingerprint(line) {
		return Stale, ErrStaleConsent
	}
	return Granted, nil
}

// GrantLaneInstallConsent records install consent for one lane, leaving every
// other lane's install grant — and every lane's TEST grant, this one included —
// exactly as it was.
func GrantLaneInstallConsent(store Store, name, line string) error {
	l, err := store.Load()
	if err != nil {
		return err
	}
	if l.TrustedLaneInstalls == nil {
		l.TrustedLaneInstalls = map[string]string{}
	}
	l.TrustedLaneInstalls[name] = Fingerprint(line)
	return store.Save(l)
}

// RevokeLaneInstallConsent drops one lane's install grant.
//
// A missing entry is not an error, on RevokeLaneConsent's precedent: the caller
// is expressing "this lane has no install grant", and that is already true.
func RevokeLaneInstallConsent(store Store, name string) error {
	l, err := store.Load()
	if err != nil {
		return err
	}
	if _, ok := l.TrustedLaneInstalls[name]; !ok {
		return nil
	}
	delete(l.TrustedLaneInstalls, name)
	if len(l.TrustedLaneInstalls) == 0 {
		// Back to nil so omitempty keeps an empty table out of the file — a
		// bare [trusted_lane_installs] header reads as a store that holds
		// something.
		l.TrustedLaneInstalls = nil
	}
	return store.Save(l)
}

// LaneRefusal turns one lane's consent state into the message the user
// acts on, naming the lane and the exact line in every arm.
//
// The lane name is in the text of every arm on purpose: a run refusing over
// several lanes produces several of these, and a message that only showed the
// command would leave the reader matching command lines back to blocks by eye.
func LaneRefusal(lane project.TestLane, state State, cerr error) error {
	name := lane.Name
	// EVERY line the grant covers, not just the command. One grant binds both,
	// so a refusal showing only the command would let a user re-consent to a
	// bootstrap they were never shown — and when the prepare is the line that
	// changed, the message would display text that did not change while asking
	// them to approve text they cannot see.
	lines, what := "    "+lane.Command+"\n", "command"
	if lane.Prepare != "" {
		lines = "    prepare: " + lane.Prepare + "\n    command: " + lane.Command + "\n"
		what = "prepare or command"
	}
	// The vouch setting joins the list because turning it on stales the grant
	// with no line edited: "its command has CHANGED" would send the reader
	// looking for a difference that is not there.
	if lane.Vouch {
		lines, what = LaneVouchLines(lane), "lines or vouch setting"
	}
	switch state {
	case Refused, NotApplicable:
		return cerr
	case Stale:
		return fmt.Errorf(
			"refusing to run test lane %q: its %s has CHANGED since you trusted it —\n"+
				"the recorded consent is stale.\n\n"+
				"%s\n"+
				"Read the lines above; if that is what you meant to run, re-consent:\n\n"+
				"    dross trust --lane %s\n\n%w", name, what, lines, name, cerr)
	default:
		return fmt.Errorf(
			"refusing to run test lane %q: its %s has not been trusted on this machine.\n\n"+
				"%s\n"+
				"It comes from the repo's tracked project.toml, so a clone carries whatever\n"+
				"its author wrote. Read the lines above, then:\n\n"+
				"    dross trust --lane %s\n\n%w", name, what, lines, name, cerr)
	}
}

// LaneVouchLines renders what a vouching lane's grant says yes to, one indented
// line each, for a human about to re-consent.
//
// One renderer for the refusal and for `dross trust --lane`, so what a user is
// told to read and what they then grant cannot drift apart. The last line is the
// consequence rather than a config line: without it a grant on a vouching lane
// reads like any other, and the human never learns it opens the commit gate.
func LaneVouchLines(lane project.TestLane) string {
	var b strings.Builder
	if lane.Prepare != "" {
		b.WriteString("    prepare: " + lane.Prepare + "\n")
	}
	b.WriteString("    command: " + lane.Command + "\n")
	b.WriteString("    relate: " + lane.Relate + "\n")
	b.WriteString("    vouch: this lane may admit commits on a scoped green, with no full run\n")
	return b.String()
}

// LaneInstallRefusal turns one lane's INSTALL consent state into the message
// the user acts on.
//
// Separate wording from LaneRefusal throughout, deliberately: a user
// refused here has not been refused a test run, and a message reading like one
// would send them to `dross trust --lane` — which would grant the wrong thing
// and leave them refused again with no idea why.
func LaneInstallRefusal(lane project.TestLane, state State, cerr error) error {
	name := lane.Name
	line := "    " + lane.Install + "\n"
	switch state {
	case Refused, NotApplicable:
		return cerr
	case Stale:
		return fmt.Errorf(
			"refusing to install test lane %q's toolchain: its install line has CHANGED\n"+
				"since you trusted it — the recorded consent is stale.\n\n"+
				"%s\n"+
				"Read the line above; if that is what you meant to run, re-consent:\n\n"+
				"    dross trust --lane-install %s\n\n%w", name, line, name, cerr)
	default:
		return fmt.Errorf(
			"refusing to install test lane %q's toolchain: its install line has not been\n"+
				"trusted on this machine.\n\n"+
				"%s\n"+
				"It comes from the repo's tracked project.toml, so a clone carries whatever\n"+
				"its author wrote, and this line changes a machine rather than measuring this\n"+
				"repo. Read it above, then:\n\n"+
				"    dross trust --lane-install %s\n\n%w", name, line, name, cerr)
	}
}
