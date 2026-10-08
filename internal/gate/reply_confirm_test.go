package gate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Rivil/dross/internal/gatestate"
)

// replyDigest is a well-formed reply digest: 64 lower-case hex digits.
var replyDigest = strings.Repeat("0123456789abcdef", 4)

func replyRecord(t *testing.T, in []byte) []string {
	t.Helper()
	for _, r := range Recorders() {
		if r.Name == "reply-approval" {
			return Record(in, Env{Home: t.TempDir(), Recorders: []Recorder{r}})
		}
	}
	t.Fatal("the reply-approval recorder is not registered")
	return nil
}

func clearReply(t *testing.T, dir string) {
	t.Helper()
	if err := gatestate.ClearReplyApproval(dir); err != nil {
		t.Fatal(err)
	}
}

func TestReplyApprovalRecordsExactLabelOnly(t *testing.T) {
	dir := droot(t)
	label := ReplyApproveLabel(12, replyDigest)
	if label != "post reply #12 "+replyDigest {
		t.Fatalf("ReplyApproveLabel = %q", label)
	}
	if w := replyRecord(t, askPost(t, dir, label)); len(w) != 0 {
		t.Fatalf("recording the label warned: %v", w)
	}
	a, err := gatestate.LoadReplyApproval(dir)
	if err != nil || a == nil || a.PR != 12 || a.Digest != replyDigest || a.At.IsZero() {
		t.Fatalf("approval = %+v (err %v), want PR 12 with the digest and a time", a, err)
	}

	for _, ans := range []string{
		label + " ", label + " please", "Post reply #12 " + replyDigest, "POST REPLY #12 " + replyDigest,
		"post reply #12 " + strings.ToUpper(replyDigest), "yes, " + label, "post it", "reject", "steer",
	} {
		clearReply(t, dir)
		replyRecord(t, askPost(t, dir, ans))
		if a, err := gatestate.LoadReplyApproval(dir); a != nil || err != nil {
			t.Errorf("answer %q recorded %+v (err %v), want nothing", ans, a, err)
		}
	}

	t.Run("the label only in tool_input", func(t *testing.T) {
		clearReply(t, dir)
		replyRecord(t, askPostWith(t, dir, func(m, resp map[string]any) {
			in := m["tool_input"].(map[string]any)["answers"].(map[string]any)
			for q := range in {
				in[q] = label
			}
			out := resp["answers"].(map[string]any)
			for q := range out {
				out[q] = "steer"
			}
		}))
		if a, err := gatestate.LoadReplyApproval(dir); a != nil || err != nil {
			t.Errorf("recorded %+v (err %v) from tool_input", a, err)
		}
	})

	t.Run("an unreadable response records nothing and says so", func(t *testing.T) {
		clearReply(t, dir)
		w := replyRecord(t, askPostWith(t, dir, func(m, _ map[string]any) { delete(m, "tool_response") }))
		if a, err := gatestate.LoadReplyApproval(dir); a != nil || err != nil {
			t.Errorf("recorded %+v (err %v)", a, err)
		}
		if !strings.Contains(strings.Join(w, "\n"), "no readable answers") {
			t.Errorf("warnings %v, want one naming the unreadable response — outside an execute task nothing else reports it", w)
		}
	})

	twoAnswers := func(first, second string) []byte {
		return askPostWith(t, dir, func(_, resp map[string]any) {
			resp["answers"] = map[string]any{"Post the reply?": first, "And this one?": second}
		})
	}
	t.Run("two different reply labels are ambiguous", func(t *testing.T) {
		clearReply(t, dir)
		w := replyRecord(t, twoAnswers(label, ReplyApproveLabel(13, replyDigest)))
		if a, err := gatestate.LoadReplyApproval(dir); a != nil || err != nil {
			t.Errorf("recorded %+v (err %v) from two different approvals", a, err)
		}
		if !strings.Contains(strings.Join(w, "\n"), "ambiguous") {
			t.Errorf("warnings %v, want the ambiguity named", w)
		}
	})
	t.Run("the same label twice, or beside another answer, records it", func(t *testing.T) {
		for _, second := range []string{label, "steer"} {
			clearReply(t, dir)
			if w := replyRecord(t, twoAnswers(label, second)); len(w) != 0 {
				t.Errorf("second answer %q warned: %v", second, w)
			}
			if a, _ := gatestate.LoadReplyApproval(dir); a == nil || a.PR != 12 {
				t.Errorf("second answer %q: recorded %+v, want PR 12", second, a)
			}
		}
	})

	t.Run("outside a dross repo", func(t *testing.T) {
		bare := t.TempDir()
		replyRecord(t, askPost(t, bare, label))
		if _, err := os.Stat(filepath.Join(bare, ".dross")); !os.IsNotExist(err) {
			t.Errorf("recorded into a directory with no .dross/ (stat err %v)", err)
		}
	})
}

func TestReplyApprovalRejectsMalformed(t *testing.T) {
	dir := droot(t)
	for _, ans := range []string{
		"post reply #0 " + replyDigest,
		"post reply #-1 " + replyDigest,
		"post reply #x " + replyDigest,
		"post reply #12 xyz",
		"post reply #12 " + replyDigest[:63],
		"post reply #12 " + replyDigest + "0",
		"post reply #12 " + replyDigest[:63] + "g",
		"post reply 12 " + replyDigest,
		"post reply #012 " + replyDigest,
		"post reply #99999999999999999999999 " + replyDigest,
	} {
		clearReply(t, dir)
		replyRecord(t, askPost(t, dir, ans))
		if a, err := gatestate.LoadReplyApproval(dir); a != nil || err != nil {
			t.Errorf("malformed %q recorded %+v (err %v)", ans, a, err)
		}
	}
}

func TestReplyApprovalIsGateProtected(t *testing.T) {
	repo := droot(t)
	e := Env{Home: t.TempDir(), Gates: []Gate{mustGate(t, "tamper-guard")}, Lifted: func(Gate, string) bool { return true }}
	p := filepath.Join(repo, ".dross", "gate", gatestate.ReplyFile)
	for _, tool := range []string{"Write", "Edit"} {
		res := Check(payload(t, tool, map[string]any{"file_path": p, "content": "{}", "old_string": "a", "new_string": "b"}, repo), e)
		if res.Allowed() || !strings.Contains(res.Text(), "tamper-guard") {
			t.Errorf("%s %s: %q, want a tamper-guard refusal", tool, gatestate.ReplyFile, res.Text())
		}
	}
	for _, line := range []string{
		`echo '{"pr":12,"digest":"` + replyDigest + `"}' > .dross/gate/reply.json`,
		"printf x | tee .dross/gate/reply.json",
		"cp /tmp/r .dross/gate/reply.json",
	} {
		if res := Check(bash(t, line, repo), e); res.Allowed() {
			t.Errorf("%q passed", line)
		}
	}
}

func TestReplyApprovalStore(t *testing.T) {
	dir := droot(t)
	if a, err := gatestate.LoadReplyApproval(dir); a != nil || err != nil {
		t.Fatalf("a missing record loads %+v (err %v), want nil", a, err)
	}
	at := time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)
	first := gatestate.ReplyApproval{PR: 12, Digest: replyDigest, At: at}
	if err := gatestate.SaveReplyApproval(dir, first); err != nil {
		t.Fatal(err)
	}
	if a, err := gatestate.LoadReplyApproval(dir); err != nil || a == nil || *a != first {
		t.Fatalf("round trip = %+v (err %v), want %+v", a, err, first)
	}
	second := gatestate.ReplyApproval{PR: 13, Digest: strings.Repeat("f", 64), At: at.Add(time.Minute)}
	if err := gatestate.SaveReplyApproval(dir, second); err != nil {
		t.Fatal(err)
	}
	if a, _ := gatestate.LoadReplyApproval(dir); a == nil || *a != second {
		t.Errorf("after a second save: %+v, want it to replace the first", a)
	}
	if err := gatestate.ClearReplyApproval(dir); err != nil {
		t.Fatal(err)
	}
	if a, err := gatestate.LoadReplyApproval(dir); a != nil || err != nil {
		t.Errorf("after clear: %+v (err %v), want nil", a, err)
	}
	if err := gatestate.ClearReplyApproval(dir); err != nil {
		t.Errorf("clearing nothing: %v", err)
	}
	for _, bad := range []gatestate.ReplyApproval{{PR: 0, Digest: replyDigest}, {PR: 12, Digest: " "}} {
		if err := gatestate.SaveReplyApproval(dir, bad); err == nil {
			t.Errorf("saved %+v", bad)
		}
	}
	put(t, dir, ".dross/gate/reply.json", `{"pr":0,"digest":"x"}`)
	if a, err := gatestate.LoadReplyApproval(dir); a != nil || err == nil {
		t.Errorf("a record with no PR loaded as %+v (err %v), want an error", a, err)
	}
}
