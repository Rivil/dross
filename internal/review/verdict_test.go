package review

import (
	"strings"
	"testing"
)

func reply(json string) string {
	return "probe answers and prose first\n\n```" + VerdictFence + "\n" + json + "\n```\n"
}

func TestParseVerdict(t *testing.T) {
	ok := `{"verdict":"pass","spec":[],"quality":[]}`
	cases := []struct {
		name, reply, wantErr string
	}{
		{"zero fences", "no verdict here", "no dross-verdict fence"},
		{"two fences", reply(ok) + reply(ok), "2 dross-verdict fences"},
		{"unclosed fence", "```" + VerdictFence + "\n" + ok + "\n", "never closed"},
		{"malformed JSON", reply(`{"verdict":"pass",`), "not the verdict JSON"},
		{"unknown key", reply(`{"verdict":"pass","spec":[],"quality":[],"extra":1}`), "not the verdict JSON"},
		{"unknown severity", reply(`{"verdict":"block","spec":[],"quality":[{"severity":"MAJOR","text":"x"}]}`), `unknown severity "MAJOR"`},
		{"unknown spec severity", reply(`{"verdict":"block","spec":[{"criterion":"c-1","severity":"MAJOR","text":"x"}],"quality":[]}`), `spec finding 1 has unknown severity "MAJOR"`},
		{"missing quality severity", reply(`{"verdict":"pass","spec":[],"quality":[{"text":"x"}]}`), `unknown severity ""`},
		{"spec finding with no criterion", reply(`{"verdict":"block","spec":[{"text":"c-2 split is untested"}],"quality":[]}`), "cites no criterion"},
		{"empty spec finding text", reply(`{"verdict":"block","spec":[{"criterion":"c-1","text":"  "}],"quality":[]}`), "spec finding 1 has no text"},
		{"empty quality finding text", reply(`{"verdict":"pass","spec":[],"quality":[{"severity":"NOTE","text":""}]}`), "quality finding 1 has no text"},
		{"pass carrying a quality BLOCKING", reply(`{"verdict":"pass","spec":[],"quality":[{"severity":"BLOCKING","text":"nil deref"}]}`), `says "pass" but carries a blocking finding`},
		{"block carrying only FLAG/NOTE", reply(`{"verdict":"block","spec":[],"quality":[{"severity":"FLAG","text":"a"},{"severity":"NOTE","text":"b"}]}`), `says "block" but carries no blocking finding`},
		{"unknown verdict word", reply(`{"verdict":"maybe","spec":[],"quality":[]}`), `verdict is "maybe"`},
		{"two JSON values", reply(ok + "\n" + ok), "more than one JSON value"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v, err := ParseVerdict(c.reply, KindTask)
			if err == nil {
				t.Fatalf("parsed %+v, want an error containing %q", v, c.wantErr)
			}
			if !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("error %q does not contain %q", err, c.wantErr)
			}
			if v.Pass {
				t.Fatal("an erroring parse returned Pass=true")
			}
		})
	}

	v, err := ParseVerdict(reply(ok), KindTask)
	if err != nil || !v.Pass {
		t.Fatalf("clean pass: %+v, %v", v, err)
	}
}

func TestSpecFindingsAlwaysBlock(t *testing.T) {
	for _, sev := range []string{"FLAG", "NOTE", ""} {
		sevJSON := ""
		if sev != "" {
			sevJSON = `"severity":"` + sev + `",`
		}
		f := `{"criterion":"c-1",` + sevJSON + `"text":"pair-mode skip has no test"}`

		v, err := ParseVerdict(reply(`{"verdict":"block","spec":[`+f+`],"quality":[]}`), KindTask)
		if err != nil {
			t.Fatalf("severity %q: block with one spec finding errored: %v", sev, err)
		}
		if v.Pass {
			t.Fatalf("severity %q: a spec finding left the verdict passing", sev)
		}
		if v.Spec[0].Severity != Blocking {
			t.Fatalf("severity %q: spec finding kept severity %q, want BLOCKING", sev, v.Spec[0].Severity)
		}

		v, err = ParseVerdict(reply(`{"verdict":"pass","spec":[`+f+`],"quality":[]}`), KindTask)
		if err == nil || v.Pass {
			t.Fatalf("severity %q: a \"pass\" carrying a spec finding parsed as %+v (err %v), want an error", sev, v, err)
		}
	}
}

func TestFindingsKeepKind(t *testing.T) {
	v, err := ParseVerdict(reply(`{"verdict":"block",
		"spec":[{"criterion":"c-2","text":"S1"}],
		"quality":[{"severity":"FLAG","text":"Q1"},{"severity":"BLOCKING","text":"Q2"}]}`), KindTask)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Spec) != 1 || v.Spec[0].Text != "S1" {
		t.Fatalf("Spec = %+v, want only S1", v.Spec)
	}
	if len(v.Quality) != 2 || v.Quality[0].Text != "Q1" || v.Quality[1].Text != "Q2" {
		t.Fatalf("Quality = %+v, want Q1, Q2 in order", v.Quality)
	}
}

func TestQualityGrades(t *testing.T) {
	v, err := ParseVerdict(reply(`{"verdict":"pass","spec":[],
		"quality":[{"severity":"FLAG","text":"long func"},{"severity":"NOTE","text":"nice table"}]}`), KindTask)
	if err != nil {
		t.Fatal(err)
	}
	if !v.Pass {
		t.Fatal("FLAG and NOTE blocked the verdict: quality_blocking records them only")
	}
	if len(v.Quality) != 2 || v.Quality[0].Severity != Flag || v.Quality[1].Severity != Note {
		t.Fatalf("Quality = %+v, want the FLAG and the NOTE kept", v.Quality)
	}
}

func TestQuickSpecFindingCitesDescription(t *testing.T) {
	desc := reply(`{"verdict":"block","spec":[{"criterion":"description","text":"the --dry-run flag the quick names is missing"}],"quality":[]}`)
	if v, err := ParseVerdict(desc, KindQuick); err != nil || v.Pass || v.Spec[0].Criterion != QuickCriterion {
		t.Fatalf("quick spec finding citing %q: %+v, %v — want a blocking spec finding", QuickCriterion, v, err)
	}
	if _, err := ParseVerdict(desc, KindTask); err == nil {
		t.Fatalf("a task review accepted a spec finding citing %q", QuickCriterion)
	}
	crit := reply(`{"verdict":"block","spec":[{"criterion":"c-1","text":"x"}],"quality":[]}`)
	if _, err := ParseVerdict(crit, KindQuick); err == nil {
		t.Fatal("a quick review accepted a spec finding citing a plan criterion")
	}
}
