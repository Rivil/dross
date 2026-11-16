package cmd

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/Rivil/dross/internal/boardsync"
	"github.com/Rivil/dross/internal/changes"
	"github.com/Rivil/dross/internal/deferred"
	"github.com/Rivil/dross/internal/milestone"
	"github.com/Rivil/dross/internal/pathfence"
	"github.com/Rivil/dross/internal/phase"
	"github.com/Rivil/dross/internal/survivor"
	"github.com/Rivil/dross/internal/verify"
)

// The close-out kinds, in the fixed order a report lists them. Unverified is
// first because it is the one kind an ack cannot clear (locked
// force_unverified): it clears only via /dross-verify or --force-unverified.
const (
	closeoutUnverified = "unverified"
	closeoutNotDone    = "not-done"
	closeoutDeferred   = "deferred"
	closeoutSurvivor   = "survivor"
)

var closeoutKinds = []string{closeoutUnverified, closeoutNotDone, closeoutDeferred, closeoutSurvivor}

// closeoutItem is one unresolved thing standing between a milestone and its
// integration PR.
type closeoutItem struct {
	Kind string
	// ID is the ack item id (milestone.ParseAckItem grammar). Empty for an
	// item no ack can clear — an unverified phase.
	ID string
	// Subject is what the line is about: a phase slug, `<source>#<idx>`, or
	// the survivor's file:line.
	Subject string
	Detail  string
	// Clear names the command(s) that actually clear this item.
	Clear string
}

// closeoutError is a collector that could not finish. It is blocking and not
// ackable: a source nobody could read is not evidence that it is clean.
type closeoutError struct {
	Kind string
	Err  error
}

// closeoutReport is every collector's output. Items and Errors keep collector
// order; rendering sorts within a kind.
type closeoutReport struct {
	Items  []closeoutItem
	Errors []closeoutError
}

// closeoutCollector produces one kind's items.
type closeoutCollector struct {
	kind    string
	collect func() ([]closeoutItem, error)
}

// runCloseoutCollectors runs every collector to the end. A collector error
// becomes a blocking line under its own kind; the others still run, so a
// single refusal lists everything rather than stopping at the first failing
// kind (c-4).
func runCloseoutCollectors(cs []closeoutCollector) closeoutReport {
	var r closeoutReport
	for _, c := range cs {
		items, err := c.collect()
		r.Items = append(r.Items, items...)
		if err != nil {
			r.Errors = append(r.Errors, closeoutError{Kind: c.kind, Err: err})
		}
	}
	return r
}

// milestoneCloseout collects every unresolved close-out item for a milestone.
// No milestone record, or one with no phases, has nothing to close out (c-8);
// a record that will not parse is an error, because without it there is no
// phase list to check against.
func milestoneCloseout(root, version string) (closeoutReport, error) {
	path := milestone.FilePath(root, version)
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		return closeoutReport{}, nil
	}
	m, err := milestone.Load(path)
	if err != nil {
		return closeoutReport{}, fmt.Errorf("load milestone %s: %w", version, err)
	}
	phases := dedupePhases(m.Phases)
	if len(phases) == 0 {
		return closeoutReport{}, nil
	}
	return runCloseoutCollectors([]closeoutCollector{
		{closeoutUnverified, func() ([]closeoutItem, error) { return closeoutUnverifiedItems(root, version) }},
		{closeoutNotDone, func() ([]closeoutItem, error) { return closeoutNotDoneItems(root, phases), nil }},
		{closeoutDeferred, func() ([]closeoutItem, error) { return closeoutDeferredItems(root, phases) }},
		{closeoutSurvivor, func() ([]closeoutItem, error) { return closeoutSurvivorItems(root, phases) }},
	}), nil
}

// dedupePhases keeps roadmap order and drops repeats: a phase listed twice is
// one phase to close out.
func dedupePhases(ids []string) []string {
	seen := make(map[string]bool, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// routedSurvivorText is the text `dross survivor route` files a routed survivor
// with (mirrors boardsync's survivorText), parsed back so the accept hint
// carries the arguments the command actually takes.
var routedSurvivorText = regexp.MustCompile(`^survivor (.+):(\d+) \(([^)]+)\)$`)

func ackHint(id string) string {
	return fmt.Sprintf("`dross milestone ack %s --reason \"...\"`", id)
}

// closeoutUnverifiedItems wraps the existing verify gate. Its items carry no
// ack id: an unverified phase clears only by verifying it or by
// --force-unverified, never by an acknowledgement.
func closeoutUnverifiedItems(root, version string) ([]closeoutItem, error) {
	bad, err := milestoneUnverifiedPhases(root, version)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []closeoutItem
	for _, b := range bad {
		if seen[b.id] {
			continue
		}
		seen[b.id] = true
		out = append(out, closeoutItem{
			Kind:    closeoutUnverified,
			Subject: b.id,
			Detail:  b.reason,
			Clear:   fmt.Sprintf("/dross-verify %s, or pass --force-unverified", b.id),
		})
	}
	return out, nil
}

// closeoutNotDoneItems names every milestone phase without a shipped or
// complete record. An unscaffolded roadmap entry is not done, and so is a
// phase whose changes.json will not parse: an unreadable record is not
// evidence of completion.
//
// Doneness is phase.Done — the single doneness reader `dross status` and
// `milestone progress` use — so close-out cannot drift into its own definition.
// The detail line only explains a not-done answer Done already gave.
func closeoutNotDoneItems(root string, phases []string) []closeoutItem {
	var out []closeoutItem
	for _, id := range phases {
		if phase.Done(root, id) {
			continue
		}
		detail := notDoneDetail(root, id)
		ackID := milestone.FormatAckItem(milestone.AckItem{Kind: milestone.AckPhase, Payload: id})
		out = append(out, closeoutItem{
			Kind:    closeoutNotDone,
			ID:      ackID,
			Subject: id,
			Detail:  detail,
			Clear:   fmt.Sprintf("finish it (/dross-execute, /dross-ship %s), or %s", id, ackHint(ackID)),
		})
	}
	return out
}

// notDoneDetail says why a phase is not done. The id is contained before
// anything is stat'd, so a phases entry like ../x never reaches a path outside
// .dross/phases; only a missing directory reads as unscaffolded, and any other
// stat failure is named as itself.
func notDoneDetail(root, id string) string {
	c, err := phase.ContainID(root, id)
	if err != nil {
		return fmt.Sprintf("phase id refused (%v)", err)
	}
	fi, err := pathfence.Stat(c)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "no phase directory — unscaffolded roadmap entry"
	case err != nil:
		return fmt.Sprintf("phase directory unreadable (%v)", err)
	case !fi.IsDir():
		return "phases entry is not a directory"
	}
	ch, err := changes.Load(changes.FilePath(root, id), id)
	switch {
	case err != nil:
		return fmt.Sprintf("changes.json will not parse (%v)", err)
	case ch.Status == "":
		return "no shipped or complete record"
	}
	return fmt.Sprintf("status %q", ch.Status)
}

// closeoutDeferredItems lists every deferred item routed to a milestone phase
// that no record has taken in. It only reads: a pre-identity item is listed
// as it is, never stamped with an id (no EnsureIDs mint), so its source spec
// stays byte-identical.
//
// A plain item clears when a criterion of its target's spec lists its id (the
// `dross deferred absorb` record). A routed survivor is not absorbed by a
// criterion; it clears only when boardsync.Disposed holds — survivors.toml
// accepts its key, or the target's own finalized, newer verify run measured
// the file and no longer lists it.
func closeoutDeferredItems(root string, phases []string) ([]closeoutItem, error) {
	entries, err := deferred.Collect(root)
	if err != nil {
		return nil, fmt.Errorf("read deferred items: %w", err)
	}
	inMilestone := make(map[string]bool, len(phases))
	for _, id := range phases {
		inMilestone[id] = true
	}
	absorbed := map[string]map[string]bool{} // target → absorbed ids
	var out []closeoutItem
	for _, e := range entries {
		if e.Dismissed || e.Target == "" || !inMilestone[e.Target] {
			continue
		}
		ackID := milestone.FormatAckItem(milestone.AckItem{Kind: milestone.AckDeferred, Source: e.Source, Index: e.Index})
		item := closeoutItem{
			Kind:    closeoutDeferred,
			ID:      ackID,
			Subject: fmt.Sprintf("%s#%d", e.Source, e.Index),
			Detail:  fmt.Sprintf("%s deferred[%d] → %s: %s", e.Source, e.Index, e.Target, e.Text),
		}
		if e.Survivor != "" {
			if ok, _ := boardsync.Disposed(root, e); ok {
				continue
			}
			where, op := "<file>:<line>", "<op>"
			if m := routedSurvivorText.FindStringSubmatch(e.Text); m != nil {
				where, op = m[1]+":"+m[2], m[3]
			}
			item.Clear = fmt.Sprintf("`dross survivor accept %s --op %s --reason \"...\"` (survivor %s), or re-verify %s (/dross-verify %s), or %s",
				where, op, e.Survivor, e.Target, e.Target, ackHint(ackID))
			out = append(out, item)
			continue
		}
		ids, ok := absorbed[e.Target]
		if !ok {
			ids, err = absorbedIDs(root, e.Target)
			if err != nil {
				return out, err
			}
			absorbed[e.Target] = ids
		}
		if e.ID != "" && ids[e.ID] {
			continue
		}
		item.Clear = fmt.Sprintf("`dross deferred absorb %s %d --phase %s --criterion <c-N>`, or %s",
			e.Source, e.Index, e.Target, ackHint(ackID))
		out = append(out, item)
	}
	return out, nil
}

// absorbedIDs returns the deferred ids a phase's spec criteria absorbed. An
// unscaffolded phase (no spec yet) has absorbed nothing; a spec that exists
// but will not parse is an error.
func absorbedIDs(root, phaseID string) (map[string]bool, error) {
	path := filepath.Join(phase.Dir(root, phaseID), "spec.toml")
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		return map[string]bool{}, nil
	}
	spec, err := phase.LoadSpec(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	ids := map[string]bool{}
	for _, c := range spec.Criteria {
		for _, id := range c.Deferred {
			ids[id] = true
		}
	}
	return ids, nil
}

// survivorGroup is one survivor as recorded by one or more milestone phases.
type survivorGroup struct {
	rec       verify.SurvivorRecord
	ambiguous bool
	phases    []string
}

// closeoutSurvivorItems re-classifies every verify.toml survivor record of the
// milestone's phases against the current accept and route stores (locked
// survivor_source). A keyed record clears when survivors.toml accepts its key
// — unless the run marked it ambiguous, when only a route settles it — or when
// a targeted, non-dismissed deferred entry routes its key. A keyless record
// has no key to accept or route by, so only an ack clears it.
func closeoutSurvivorItems(root string, phases []string) ([]closeoutItem, error) {
	store, err := survivor.Load(survivor.Path(root))
	if err != nil {
		return nil, err
	}
	accepted, err := acceptedReasons(store)
	if err != nil {
		return nil, err
	}
	routed, err := routedSurvivors(root)
	if err != nil {
		return nil, fmt.Errorf("read routed survivors: %w", err)
	}

	var order []string
	groups := map[string]*survivorGroup{}
	var errs []error
	for _, id := range phases {
		_, vToml := verify.FilePaths(root, id)
		v, err := verify.LoadVerify(vToml)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", id, err))
			continue
		}
		if v == nil {
			continue
		}
		for _, r := range v.Survivors {
			payload := r.Key
			if payload == "" {
				payload = fmt.Sprintf("%s:%d:%s", r.File, r.Line, r.Op)
			}
			ackID := milestone.FormatAckItem(milestone.AckItem{Kind: milestone.AckSurvivor, Payload: payload})
			g, ok := groups[ackID]
			if !ok {
				g = &survivorGroup{rec: r}
				groups[ackID] = g
				order = append(order, ackID)
			}
			g.ambiguous = g.ambiguous || r.Ambiguous
			if !slices.Contains(g.phases, id) {
				g.phases = append(g.phases, id)
			}
		}
	}

	var out []closeoutItem
	for _, ackID := range order {
		g := groups[ackID]
		key := g.rec.Key
		if key != "" {
			if _, ok := routed[key]; ok {
				continue
			}
			if _, ok := accepted[key]; ok && !g.ambiguous {
				continue
			}
		}
		where := fmt.Sprintf("%s:%d", g.rec.File, g.rec.Line)
		detail := fmt.Sprintf("%s (%s) recorded by %s", where, g.rec.Op, strings.Join(g.phases, ", "))
		var clear string
		switch {
		case key == "":
			detail += " — no survivor key"
			clear = ackHint(ackID)
		case g.ambiguous:
			detail += " — ambiguous: an acceptance cannot settle it"
			clear = fmt.Sprintf("`dross survivor route %s --op %s --target <phase>`, or %s", where, g.rec.Op, ackHint(ackID))
		default:
			clear = fmt.Sprintf("`dross survivor accept %s --op %s --reason \"...\"` or `dross survivor route %s --op %s --target <phase>`, or %s",
				where, g.rec.Op, where, g.rec.Op, ackHint(ackID))
		}
		out = append(out, closeoutItem{
			Kind:    closeoutSurvivor,
			ID:      ackID,
			Subject: where,
			Detail:  detail,
			Clear:   clear,
		})
	}
	return out, errors.Join(errs...)
}
