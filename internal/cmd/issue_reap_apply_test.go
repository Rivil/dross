package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/reaplog"
)

// applyYT is a writable YouTrack stand-in. It models the one property every
// assertion here turns on: a close is a State write FOLLOWED BY a read, and the
// read is what decides whether the close happened.
type applyYT struct {
	state    map[string]string // key -> the column the card is in
	resolved map[string]bool
	labels   map[string][]string
	// refuse names cards whose State write returns 500 — a tracker outage
	// mid-sweep.
	refuse map[string]bool
	// ignore names cards whose State write is ACCEPTED and changes nothing:
	// YouTrack's own failure mode, where a workflow takes the POST and refuses
	// the transition. A 200 is not evidence.
	ignore map[string]bool

	wrote map[string][]string // key -> every State value written to it
	// refuseTags makes every tag write fail, to prove a refused relabel does
	// not demote a verified close.
	refuseTags bool
}

func newApplyYT() *applyYT {
	return &applyYT{
		state:    map[string]string{},
		resolved: map[string]bool{},
		labels:   map[string][]string{},
		refuse:   map[string]bool{},
		ignore:   map[string]bool{},
		wrote:    map[string][]string{},
	}
}

// knownTags is the tracker's tag index: every label any card carries, plus the
// status labels the sweep writes.
func (f *applyYT) knownTags() []string {
	seen := map[string]bool{}
	var out []string
	add := func(n string) {
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	add(labelMarker)
	for _, ls := range f.labels {
		for _, l := range ls {
			add(l)
		}
	}
	for _, lane := range reapLanes {
		add(statusLabel(lane.Terminal))
	}
	return out
}

func (f *applyYT) seed(key, state string, labels ...string) {
	f.state[key] = state
	f.labels[key] = labels
}

func (f *applyYT) render(key string) string {
	resolved := "null"
	if f.resolved[key] {
		resolved = "1700000000000"
	}
	var tags []string
	for _, l := range f.labels[key] {
		tags = append(tags, `{"id":"tid-`+l+`","name":"`+l+`"}`)
	}
	return `{"idReadable":"` + key + `","resolved":` + resolved +
		`,"tags":[` + strings.Join(tags, ",") + `]` +
		`,"customFields":[{"name":"State","value":{"name":"` + f.state[key] + `"}}]}`
}

func (f *applyYT) handler(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/issueTags" && r.Method == "GET":
			var rows []string
			for _, n := range f.knownTags() {
				rows = append(rows, `{"id":"tid-`+n+`","name":"`+n+`"}`)
			}
			_, _ = io.WriteString(w, "["+strings.Join(rows, ",")+"]")
		case r.URL.Path == "/api/issues" && r.Method == "GET":
			_, _ = io.WriteString(w, `[]`)
		case strings.HasPrefix(r.URL.Path, "/api/issues/") && r.Method == "GET":
			_, _ = io.WriteString(w, f.render(strings.TrimPrefix(r.URL.Path, "/api/issues/")))
		case r.URL.Path == "/api/issueTags" && r.Method == "POST":
			_, _ = io.WriteString(w, `{"id":"tid-x"}`)
		case strings.HasSuffix(r.URL.Path, "/tags") && r.Method == "POST":
			if f.refuseTags {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = io.WriteString(w, `{"error":"no"}`)
				return
			}
			key := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/issues/"), "/tags")
			var tag struct {
				ID string `json:"id"`
			}
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &tag)
			f.labels[key] = append(f.labels[key], strings.TrimPrefix(tag.ID, "tid-"))
			_, _ = io.WriteString(w, `{}`)
		case strings.Contains(r.URL.Path, "/tags/") && r.Method == "DELETE":
			if f.refuseTags {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = io.WriteString(w, `{"error":"no"}`)
				return
			}
			parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/api/issues/"), "/tags/", 2)
			if len(parts) == 2 {
				var kept []string
				for _, l := range f.labels[parts[0]] {
					if "tid-"+l != parts[1] && l != parts[1] {
						kept = append(kept, l)
					}
				}
				f.labels[parts[0]] = kept
			}
			_, _ = io.WriteString(w, `{}`)
		case strings.HasPrefix(r.URL.Path, "/api/issues/") && r.Method == "POST":
			key := strings.TrimPrefix(r.URL.Path, "/api/issues/")
			var body struct {
				CustomFields []struct {
					Name  string `json:"name"`
					Value struct {
						Name string `json:"name"`
					} `json:"value"`
				} `json:"customFields"`
			}
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &body)
			for _, cf := range body.CustomFields {
				if cf.Name != "State" {
					continue
				}
				f.wrote[key] = append(f.wrote[key], cf.Value.Name)
				if f.refuse[key] {
					w.WriteHeader(http.StatusInternalServerError)
					_, _ = io.WriteString(w, `{"error":"boom"}`)
					return
				}
				if f.ignore[key] {
					continue // accepted, changed nothing
				}
				f.state[key] = cf.Value.Name
				f.resolved[key] = true
			}
			_, _ = io.WriteString(w, `{"idReadable":"`+key+`"}`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			_, _ = io.WriteString(w, `{}`)
		}
	}
}

// applyRepo scaffolds the all-five-lanes fixture against a writable fake.
//
// The state map is overridden so the phase lane's `complete` and the task
// lane's `task-complete` land in DIFFERENT columns. YouTrack's defaults map
// both to "Verified", which would make "each card was written its own lane's
// terminal" unfalsifiable on the wire — a single shared terminal would satisfy
// the assertion.
func applyRepo(t *testing.T, f *applyYT) string {
	t.Helper()
	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)
	dir := youtrackBoardRepo(t, srv.URL)
	mustRunSet(t, "board.milestone_mode", "epic")
	mustRunSet(t, "board.state_map.complete", "Verified")
	mustRunSet(t, "board.state_map.task-complete", "Task Done")
	mustWrite(t, filepath.Join(dir, ".dross", "board.json"), strandedBoard)
	writeStrandedFixture(t, dir)
	for _, k := range []string{"PROJ-1", "PROJ-2", "PROJ-7", "PROJ-20", "PROJ-40"} {
		f.seed(k, "In Review", labelMarker)
	}
	return dir
}

func loadReapLog(t *testing.T, dir string) *reaplog.Log {
	t.Helper()
	l, err := reaplog.Load(reaplog.FilePath(filepath.Join(dir, ".dross")))
	if err != nil {
		t.Fatalf("load reap log: %v", err)
	}
	return l
}

// TestReapApplyClosesEveryLane: every closable lane reaches its OWN terminal.
// One shared terminal for the whole run would leave every task card in the
// phase lane's column.
func TestReapApplyClosesEveryLane(t *testing.T) {
	f := newApplyYT()
	applyRepo(t, f)

	_ = captureStdout(t, func() {
		if err := runCmd(t, Issue(), "reap", "--apply"); err != nil {
			t.Fatalf("reap --apply: %v", err)
		}
	})

	for _, tc := range []struct{ key, want string }{
		{"PROJ-1", "Verified"},  // Phases   -> complete
		{"PROJ-2", "Task Done"}, // Tasks    -> task-complete
		{"PROJ-7", "Verified"},  // Milestones
		{"PROJ-20", "Verified"}, // Backlog
	} {
		if !f.resolved[tc.key] {
			t.Errorf("%s is still unresolved after apply", tc.key)
		}
		if got := f.wrote[tc.key]; len(got) != 1 || got[0] != tc.want {
			t.Errorf("%s was written %v, want its own lane terminal %q", tc.key, got, tc.want)
		}
	}
	// The quick lane is classified but never auto-closed: no completion record
	// exists for a quick.
	if f.resolved["PROJ-40"] {
		t.Error("the sweep closed a quick — nothing on disk can show a quick finished")
	}
}

// TestApplyContinuesPastAWriteFailure is the defect this phase exists to fix,
// one level up: one stuck card must not strand every card after it.
func TestApplyContinuesPastAWriteFailure(t *testing.T) {
	f := newApplyYT()
	applyRepo(t, f)
	f.refuse["PROJ-2"] = true

	var err error
	out := captureStdout(t, func() {
		errOut := captureStderr(t, func() {
			err = runCmd(t, Issue(), "reap", "--apply")
		})
		// One REPORT LINE per failure. The wrapped tracker error names the
		// key too, so counting substrings would measure the error's prose
		// rather than the report's shape.
		lines := 0
		for _, l := range strings.Split(errOut, "\n") {
			if strings.Contains(l, "PROJ-2") {
				lines++
			}
		}
		if lines != 1 {
			t.Errorf("the failing card gets %d report lines, want exactly one:\n%s", lines, errOut)
		}
	})
	if err == nil {
		t.Fatal("a run with a refused close exited 0 — a half-swept board must not look like a swept one")
	}
	if f.resolved["PROJ-2"] {
		t.Error("the refused card was counted resolved")
	}
	for _, still := range []string{"PROJ-1", "PROJ-7", "PROJ-20"} {
		if !f.resolved[still] {
			t.Errorf("%s was never attempted — one stuck card stranded the rest:\n%s", still, out)
		}
	}
}

// TestUnverifiedCloseIsNotCountedClosed: YouTrack can accept the POST and
// refuse the transition. A 200 is not evidence the card moved.
func TestUnverifiedCloseIsNotCountedClosed(t *testing.T) {
	f := newApplyYT()
	dir := applyRepo(t, f)
	f.ignore["PROJ-1"] = true

	var err error
	_ = captureStdout(t, func() {
		_ = captureStderr(t, func() {
			err = runCmd(t, Issue(), "reap", "--apply")
		})
	})
	if err == nil {
		t.Fatal("a close the workflow silently refused was reported as success")
	}
	for _, c := range loadReapLog(t, dir).Last().Closed() {
		if c.Issue == "PROJ-1" {
			t.Error("an unverified close was journalled as closed — undo would then move a card the sweep never moved")
		}
	}
}

// TestReapLogCapturesStateBeforeTheWrite is the ledger's whole value. A journal
// built from the post-close read-back records the terminal state, and undo
// restores every card to the column it is already in — a no-op that reports
// success.
func TestReapLogCapturesStateBeforeTheWrite(t *testing.T) {
	f := newApplyYT()
	dir := applyRepo(t, f)

	_ = captureStdout(t, func() {
		if err := runCmd(t, Issue(), "reap", "--apply"); err != nil {
			t.Fatalf("reap --apply: %v", err)
		}
	})

	run := loadReapLog(t, dir).Last()
	if run == nil {
		t.Fatal("no run was journalled")
	}
	seen := 0
	for _, c := range run.Closed() {
		seen++
		if c.PriorState != "In Review" {
			t.Errorf("%s journalled prior_state %q, want the column it held BEFORE the close", c.Issue, c.PriorState)
		}
		if c.PriorResolved {
			t.Errorf("%s journalled prior_resolved=true — it was open before the sweep", c.Issue)
		}
	}
	if seen == 0 {
		t.Fatal("the journal recorded no closed cards — the assertion above is vacuous")
	}
	// The backlog lane's forward path drops its board.json key, so the sweep
	// does too — and records the drop, since by undo time board.json no longer
	// holds it.
	found := false
	for _, c := range run.Closed() {
		if c.Issue == "PROJ-20" {
			found = true
			if c.DroppedLink != "slug:built" {
				t.Errorf("PROJ-20 journalled dropped_link %q, want the backlog key the sweep deleted", c.DroppedLink)
			}
		}
	}
	if !found {
		t.Error("the backlog card is not in the journal")
	}
	bd := mustRead(t, filepath.Join(dir, ".dross", "board.json"))
	if strings.Contains(bd, "slug:built") {
		t.Error("the backlog link was journalled as dropped but is still in board.json")
	}
}

// TestJournalRecordsOnlyRealCloses: undo walks Closed(), and a card that never
// moved must not be in it.
func TestJournalRecordsOnlyRealCloses(t *testing.T) {
	f := newApplyYT()
	dir := applyRepo(t, f)
	f.refuse["PROJ-2"] = true

	_ = captureStdout(t, func() {
		_ = captureStderr(t, func() {
			_ = runCmd(t, Issue(), "reap", "--apply")
		})
	})

	for _, c := range loadReapLog(t, dir).Last().Closed() {
		if c.Issue == "PROJ-2" {
			t.Error("a card whose close was refused is in the undo set — restoring it would write a state the sweep never caused")
		}
	}
}

// TestSecondApplyIsANoOp is c-4 end to end: after a full sweep the board and
// the records agree, so there is nothing left to classify.
func TestSecondApplyIsANoOp(t *testing.T) {
	f := newApplyYT()
	applyRepo(t, f)

	_ = captureStdout(t, func() {
		if err := runCmd(t, Issue(), "reap", "--apply"); err != nil {
			t.Fatalf("first apply: %v", err)
		}
	})
	before := map[string]int{}
	for k, v := range f.wrote {
		before[k] = len(v)
	}

	out := captureStdout(t, func() {
		if err := runCmd(t, Issue(), "reap", "--apply"); err != nil {
			t.Fatalf("second apply: %v", err)
		}
	})
	for k, n := range before {
		if len(f.wrote[k]) != n {
			t.Errorf("%s was written again on the second apply (%d -> %d)", k, n, len(f.wrote[k]))
		}
	}
	// "Zero stranded", not "empty plan": the quick card is unattributable and
	// stays listed until a human closes it, which is the point of listing it.
	// c-4's claim is about what the sweep would CLOSE.
	if !strings.Contains(out, "0 stranded") {
		t.Errorf("second run still classifies cards as stranded:\n%s", out)
	}
	if strings.Contains(out, "PROJ-1") || strings.Contains(out, "PROJ-20") {
		t.Errorf("a card the first run closed is still in the plan:\n%s", out)
	}
}

// TestReapNeverTouchesCorrectlyOpenCards: c-1's other half. A card whose record
// is not complete, and a card carrying no dross marker, are still open after a
// whole-board apply.
func TestReapNeverTouchesCorrectlyOpenCards(t *testing.T) {
	f := newApplyYT()
	dir := applyRepo(t, f)
	// A live phase, mirrored — its record shows no completion.
	mustWrite(t, filepath.Join(dir, ".dross", "board.json"), `{
	  "phases": {"01-auth": "PROJ-1", "02-live": "PROJ-50"},
	  "tasks": {}, "milestones": {}, "quicks": {}, "backlog": {}
	}`)
	writeChanges(t, dir, "02-live", "")
	f.seed("PROJ-50", "In Progress", labelMarker)
	f.seed("PROJ-77", "Open") // a human's issue: no marker, in no namespace

	_ = captureStdout(t, func() {
		if err := runCmd(t, Issue(), "reap", "--apply"); err != nil {
			t.Fatalf("reap --apply: %v", err)
		}
	})
	if f.resolved["PROJ-50"] {
		t.Error("a phase whose record shows no completion was closed")
	}
	if len(f.wrote["PROJ-77"]) != 0 || f.resolved["PROJ-77"] {
		t.Error("a human-filed card was written to")
	}
	if !f.resolved["PROJ-1"] {
		t.Error("the genuinely stranded card was not closed — the fixture proves nothing")
	}
}

// TestReapedCardCarriesTheLaneTerminalLabel: the state write is only half of
// what the forward path does. A card ship closes carries
// `dross/status:<terminal>` too, and the locked reap_state decision is that a
// reaped card lands where the forward path puts it — the point being that the
// board reads as one history.
//
// Found on the live board: reaped task cards sat Verified under a stale
// `dross/status:task-in-review`, permanently distinguishable from
// forward-closed ones, and no later sweep would revisit them because a resolved
// card is no longer stranded.
func TestReapedCardCarriesTheLaneTerminalLabel(t *testing.T) {
	f := newApplyYT()
	applyRepo(t, f)
	// The state the execute loop leaves a task card in.
	f.labels["PROJ-2"] = []string{labelMarker, taskLabel("01-auth", "t-1"), statusLabel(statusTaskInReview)}
	f.labels["PROJ-1"] = []string{labelMarker, phaseLabel("01-auth"), statusLabel(statusInProgress)}

	_ = captureStdout(t, func() {
		_ = captureStderr(t, func() {
			if err := runCmd(t, Issue(), "reap", "--apply"); err != nil {
				t.Fatalf("reap --apply: %v", err)
			}
		})
	})

	for _, tc := range []struct{ key, want string }{
		{"PROJ-2", statusLabel(statusTaskComplete)},
		{"PROJ-1", statusLabel("complete")},
	} {
		if !slicesHas(f.labels[tc.key], tc.want) {
			t.Errorf("%s labels = %v, want the lane terminal %q", tc.key, f.labels[tc.key], tc.want)
		}
		for _, l := range f.labels[tc.key] {
			if strings.HasPrefix(l, "dross/status:") && l != tc.want {
				t.Errorf("%s still carries the stale status label %q", tc.key, l)
			}
		}
	}
	// The identity labels the discovery sweep depends on must survive.
	if !slicesHas(f.labels["PROJ-2"], taskLabel("01-auth", "t-1")) || !slicesHas(f.labels["PROJ-2"], labelMarker) {
		t.Errorf("PROJ-2 lost an identity label: %v", f.labels["PROJ-2"])
	}
}

// TestRelabelFailureDoesNotFailTheCard: the close is the load-bearing write and
// it has already been verified. Downgrading a closed card to a failure over an
// annotation would put it in the undo set and take it out of the closed count
// for something the operator can see is done.
func TestRelabelFailureDoesNotFailTheCard(t *testing.T) {
	f := newApplyYT()
	dir := applyRepo(t, f)
	f.refuseTags = true

	var err error
	_ = captureStdout(t, func() {
		errOut := captureStderr(t, func() {
			err = runCmd(t, Issue(), "reap", "--apply")
		})
		if !strings.Contains(errOut, "could not update its status label") {
			t.Errorf("the relabel failure was swallowed rather than warned about:\n%s", errOut)
		}
	})
	if err != nil {
		t.Fatalf("a refused relabel failed the run: %v", err)
	}
	if !f.resolved["PROJ-1"] {
		t.Error("the card was not closed")
	}
	found := false
	for _, c := range loadReapLog(t, dir).Last().Closed() {
		if c.Issue == "PROJ-1" {
			found = true
		}
	}
	if !found {
		t.Error("a closed card was dropped from the undo set because its label write failed")
	}
}
