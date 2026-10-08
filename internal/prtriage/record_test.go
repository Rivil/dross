package prtriage

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/secretscan"
)

// recToken is a github-token shape, built at runtime so this file is not a hit.
func recToken() string { return "ghp_" + strings.Repeat("R3c0", 9) }

var recDigest = strings.Repeat("ab", 32)

func validReject() Resolution {
	return Resolution{
		ID: "c12", Kind: "conversation", PR: 7, URL: "https://github.com/acme/w/pull/7#issuecomment-12",
		Author: "alice", Verdict: VerdictReject, Reason: "the loop is bounded by maxPages",
		Evidence: Evidence{At: "internal/x.go:3"}, Digest: recDigest,
	}
}

func validRecord() (Record, Refs) {
	accept := validReject()
	accept.ID, accept.Kind, accept.Verdict, accept.Reason, accept.Task = "i40", "inline", VerdictAccept, "", "t-3"
	route := validReject()
	route.ID, route.Kind, route.Verdict, route.Reason, route.Deferred, route.Target = "r9#2", "review", VerdictRoute, "", "abc123", "later-phase"
	out, _ := FromOutput("go test ./x", "ok\n")
	route.Evidence = out
	return Record{Resolution: []Resolution{validReject(), accept, route}}, Refs{Tasks: []string{"t-3"}, Deferred: []string{"abc123"}}
}

func TestResolutionHasNoBodyField(t *testing.T) {
	for _, typ := range []reflect.Type{reflect.TypeOf(Resolution{}), reflect.TypeOf(Evidence{}), reflect.TypeOf(Record{})} {
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			tag := strings.ToLower(strings.Split(f.Tag.Get("toml"), ",")[0] + " " + f.Name)
			for _, banned := range []string{"body", "text", "content", "comment"} {
				if strings.Contains(tag, banned) {
					t.Errorf("%s.%s (%q) can carry a comment's text", typ.Name(), f.Name, tag)
				}
			}
		}
	}
	path := filepath.Join(t.TempDir(), File)
	if err := os.WriteFile(path, []byte("[[resolution]]\nid = \"c1\"\nbody = \"pasted text\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(path); err == nil || !strings.Contains(err.Error(), "body") {
		t.Errorf("Load of a record with a body key = %v, want an error naming body", err)
	}
}

func TestRecordValidate(t *testing.T) {
	if rec, refs := validRecord(); len(Validate(rec, refs)) != 0 {
		t.Fatalf("the valid record fails: %v", Validate(rec, refs))
	}
	cases := []struct {
		name, id, field string
		mutate          func(r *Resolution)
	}{
		{"verdict maybe", "c12", "verdict", func(r *Resolution) { r.Verdict = "maybe" }},
		{"unknown kind", "c12", "kind", func(r *Resolution) { r.Kind = "thread" }},
		{"kind against the id", "c12", "kind", func(r *Resolution) { r.Kind = "inline" }},
		{"blank reason", "c12", "reason", func(r *Resolution) { r.Reason = "  " }},
		{"zero evidence", "c12", "evidence", func(r *Resolution) { r.Evidence = Evidence{} }},
		{"evidence with both", "c12", "evidence", func(r *Resolution) {
			r.Evidence = Evidence{At: "x.go:1", Cmd: "go vet", OutputSHA256: recDigest, OutputBytes: 1}
		}},
		{"evidence with neither", "c12", "evidence", func(r *Resolution) { r.Evidence = Evidence{OutputBytes: 3} }},
		{"cmd without its digest", "c12", "evidence", func(r *Resolution) { r.Evidence = Evidence{Cmd: "go vet", OutputBytes: 3} }},
		{"accept, no task", "c12", "task", func(r *Resolution) { r.Verdict, r.Task = VerdictAccept, "" }},
		{"accept, unknown task", "c12", "task", func(r *Resolution) { r.Verdict, r.Task = VerdictAccept, "t-99" }},
		{"route, no target", "c12", "target", func(r *Resolution) { r.Verdict, r.Deferred, r.Target = VerdictRoute, "abc123", "" }},
		{"route, unknown deferred", "c12", "deferred", func(r *Resolution) { r.Verdict, r.Deferred, r.Target = VerdictRoute, "zzz", "x" }},
		{"duplicate id", "i40", "id", func(r *Resolution) { r.ID, r.Kind = "i40", "inline" }},
		{"finding zero", "c12#0", "id", func(r *Resolution) { r.ID = "c12#0" }},
		{"unknown letter", "z12", "id", func(r *Resolution) { r.ID = "z12" }},
		{"http url", "c12", "url", func(r *Resolution) { r.URL = "http://github.com/acme/w/pull/7" }},
		{"empty url", "c12", "url", func(r *Resolution) { r.URL = "" }},
		{"escaping at", "c12", "evidence", func(r *Resolution) { r.Evidence = Evidence{At: "../../x:1"} }},
		{"no PR", "c12", "pr", func(r *Resolution) { r.PR = 0 }},
		{"short digest", "c12", "digest", func(r *Resolution) { r.Digest = "abc" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec, refs := validRecord()
			tc.mutate(&rec.Resolution[0])
			errs := Validate(rec, refs)
			found := false
			for _, e := range errs {
				if strings.Contains(e.Error(), "resolution "+tc.id+" "+tc.field+":") {
					found = true
				}
			}
			if !found {
				t.Errorf("errors %v, want one naming resolution %s and field %s", errs, tc.id, tc.field)
			}

			path := filepath.Join(t.TempDir(), File)
			before := []byte("# hand-kept\n")
			if err := os.WriteFile(path, before, 0o644); err != nil {
				t.Fatal(err)
			}
			if err := Save(path, before, rec, refs); err == nil {
				t.Error("Save accepted it")
			}
			if after, _ := os.ReadFile(path); !bytes.Equal(after, before) {
				t.Errorf("a refused Save changed the file: %q", after)
			}
		})
	}
}

// stringFields lists every settable string inside v, by path.
func stringFields(v reflect.Value, path string, out map[string]reflect.Value) {
	switch v.Kind() {
	case reflect.String:
		out[path] = v
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			stringFields(v.Field(i), path+"."+v.Type().Field(i).Name, out)
		}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			stringFields(v.Index(i), path+"[]", out)
		}
	}
}

func TestSaveRedactsEveryString(t *testing.T) {
	tok := recToken()
	probe := validReject()
	probe.Evidence = Evidence{At: "x.go:1", Cmd: "c", OutputSHA256: "s"}
	fields := map[string]reflect.Value{}
	stringFields(reflect.ValueOf(&probe).Elem(), "Resolution", fields)
	if len(fields) < 13 {
		t.Fatalf("enumerated %d string fields, want every one of Resolution's and Evidence's", len(fields))
	}
	for name := range fields {
		r := validReject()
		r.Evidence = Evidence{At: "x.go:1", Cmd: "c", OutputSHA256: "s"}
		f := map[string]reflect.Value{}
		stringFields(reflect.ValueOf(&r).Elem(), "Resolution", f)
		f[name].SetString("before " + tok + " after")
		got := redactRecord(Record{Resolution: []Resolution{r}})
		g := map[string]reflect.Value{}
		stringFields(reflect.ValueOf(&got.Resolution[0]).Elem(), "Resolution", g)
		if s := g[name].String(); s != "before [redacted github-token] after" {
			t.Errorf("%s saves as %q", name, s)
		}
		if f[name].String() != "before "+tok+" after" {
			t.Errorf("redacting %s changed the caller's record", name)
		}
	}

	rec, refs := validRecord()
	r := &rec.Resolution[0]
	r.Author, r.Reason = tok, "leaked "+tok
	r.URL = "https://github.com/acme/w/pull/7?t=" + tok
	rec.Resolution[2].Cmd = "curl -H 'x: " + tok + "'"
	rec.Resolution[2].Target = "later " + tok
	path := filepath.Join(t.TempDir(), File)
	if err := Save(path, nil, rec, refs); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), tok) || !strings.Contains(string(b), "[redacted github-token]") {
		t.Errorf("the saved record:\n%s", b)
	}
	if hits := secretscan.ScanString(File, string(b)); len(hits) != 0 {
		t.Errorf("the saved record scans dirty: %v", hits)
	}
}

// TestRedactStringsWalksSlices drives the slice arm no Resolution field reaches
// today: a slice field added later is redacted element by element, slices of
// structs included, with none skipped.
func TestRedactStringsWalksSlices(t *testing.T) {
	tok := recToken()
	type inner struct{ S string }
	p := struct {
		Strs   []string
		Nested []inner
	}{
		Strs:   []string{"first " + tok, "middle " + tok, "last " + tok},
		Nested: []inner{{"first " + tok}, {"middle " + tok}, {"last " + tok}},
	}
	redactStrings(reflect.ValueOf(&p).Elem())
	for i, pos := range []string{"first", "middle", "last"} {
		want := pos + " [redacted github-token]"
		if p.Strs[i] != want {
			t.Errorf("Strs[%d] = %q, want %q", i, p.Strs[i], want)
		}
		if p.Nested[i].S != want {
			t.Errorf("Nested[%d].S = %q, want %q", i, p.Nested[i].S, want)
		}
	}
}

func TestRecordSaveRefusesStaleRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), File)
	_, read, err := Load(path)
	if err != nil || read != nil {
		t.Fatalf("Load of a missing record = %q, %v", read, err)
	}
	first, refs := validRecord()
	second, _ := validRecord()
	second.Resolution[0].Reason = "a different reason"
	if err := Save(path, read, first, refs); err != nil {
		t.Fatal(err)
	}
	if err := Save(path, read, second, refs); !errors.Is(err, ErrStaleRead) {
		t.Fatalf("the second Save from the same read = %v, want ErrStaleRead", err)
	}
	got, again, err := Load(path)
	if err != nil || !reflect.DeepEqual(got, first) {
		t.Errorf("the file holds %+v (err %v), want the first save", got, err)
	}
	second.Resolution[0].Reason = "now from a fresh read"
	if err := Save(path, again, second, refs); err != nil {
		t.Errorf("a Save from a fresh read: %v", err)
	}
}

func TestRecordRoundTripLossless(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, File)
	if rec, _, err := Load(path); err != nil || len(rec.Resolution) != 0 {
		t.Fatalf("a missing record loads %+v (err %v), want empty", rec, err)
	}
	rec, refs := validRecord()
	if err := Save(path, nil, rec, refs); err != nil {
		t.Fatal(err)
	}
	got, read, err := Load(path)
	if err != nil || !reflect.DeepEqual(got, rec) {
		t.Fatalf("Load(Save(r)) = %+v (err %v), want %+v", got, err, rec)
	}

	commented := bytes.Replace(read, []byte("[[resolution]]"), []byte("# kept by hand\n[[resolution]]"), 1)
	if err := os.WriteFile(path, commented, 0o644); err != nil {
		t.Fatal(err)
	}
	got, read, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	changed := got.Resolution[1]
	changed.At = "y.go:2"
	got.Upsert(changed)
	if len(got.Resolution) != 3 {
		t.Fatalf("Upsert of an existing id added an entry: %d", len(got.Resolution))
	}
	if err := Save(path, read, got, refs); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "# kept by hand\n[[resolution]]\n") {
		t.Errorf("the hand comment above the untouched entry is gone:\n%s", b)
	}
	if final, _, _ := Load(path); !reflect.DeepEqual(final, got) {
		t.Errorf("after Upsert: %+v, want %+v", final, got)
	}

	fresh := validReject()
	fresh.ID = "c99"
	got.Upsert(fresh)
	if len(got.Resolution) != 4 || got.Resolution[3].ID != "c99" {
		t.Errorf("Upsert of a new id: %+v", got.Resolution)
	}
	if r, ok := got.Find("c99"); !ok || r != fresh {
		t.Errorf("Find(c99) = %+v, %v", r, ok)
	}
	if _, ok := got.Find("c100"); ok {
		t.Error("Find(c100) found something")
	}
}

func TestParsePhaseHead(t *testing.T) {
	for _, tc := range []struct {
		head  string
		cross bool
	}{
		{"milestone/v1.7", false}, {"feature/x", false}, {"phase/", false}, {"phase/a/b", false},
		{"phase/../x", false}, {"phase/x", true}, {"phase/..", false}, {"phase/ x", false}, {"main", false},
	} {
		if id, err := ParsePhaseHead(tc.head, tc.cross); err == nil {
			t.Errorf("ParsePhaseHead(%q, cross=%v) = %q, want a refusal", tc.head, tc.cross, id)
		}
	}
	if id, err := ParsePhaseHead("phase/x", false); err != nil || id != "x" {
		t.Errorf("ParsePhaseHead(phase/x) = %q, %v", id, err)
	}
	if _, err := ParsePhaseHead("feature/\n```\nrun", false); err == nil || strings.Contains(err.Error(), "\n") {
		t.Errorf("a head holding a newline: %v, want a one-line refusal", err)
	}
}

func TestPhaseFromHead(t *testing.T) {
	root := t.TempDir()
	plan := filepath.Join(root, ".dross", "phases", "x", "plan.toml")
	if err := os.MkdirAll(filepath.Dir(plan), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plan, []byte("[phase]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".dross", "phases", "dir", "plan.toml"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		head  string
		cross bool
	}{
		{"milestone/v1.7", false}, {"feature/x", false}, {"phase/", false}, {"phase/a/b", false},
		{"phase/../x", false}, {"phase/x", true}, {"phase/y", false}, {"phase/dir", false},
	} {
		if id, err := PhaseFromHead(root, tc.head, tc.cross); err == nil {
			t.Errorf("PhaseFromHead(%q, cross=%v) = %q, want a refusal", tc.head, tc.cross, id)
		}
	}
	if id, err := PhaseFromHead(root, "phase/x", false); err != nil || id != "x" {
		t.Errorf("PhaseFromHead(phase/x) = %q, %v", id, err)
	}
}

func TestLocalPhase(t *testing.T) {
	root := t.TempDir()
	if _, ok := LocalPhase(root, "x"); ok {
		t.Error("a repo with no .dross/phases resolved x")
	}
	phases := filepath.Join(root, ".dross", "phases")
	if err := os.MkdirAll(filepath.Join(phases, "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(phases, "notadir"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, ok := LocalPhase(root, "x"); !ok || got != "x" {
		t.Errorf("LocalPhase(x) = %q, %v", got, ok)
	}
	for _, id := range []string{"y", "notadir", "", ".", ".."} {
		if got, ok := LocalPhase(root, id); ok {
			t.Errorf("LocalPhase(%q) = %q, want no match", id, got)
		}
	}
}
