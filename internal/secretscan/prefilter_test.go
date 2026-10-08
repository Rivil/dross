package secretscan_test

// Every credential-shaped value here is built at runtime, as in
// secretscan_test.go: the self-scan reads this file too.

import (
	"math/rand"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/Rivil/dross/internal/secretscan"
)

// keyContextWords is every keyword alternative in the key-context regex.
var keyContextWords = []string{
	"password", "passwd", "pwd", "secret", "api_key", "api-key",
	"access_key", "access-key", "token", "authorization",
}

// mixedCase alternates upper and lower case from an upper first letter.
func mixedCase(s string) string {
	b := []byte(strings.ToLower(s))
	for i := 0; i < len(b); i += 2 {
		b[i] = strings.ToUpper(string(b[i]))[0]
	}
	return string(b)
}

// prefilterSeeds is every line the prefilter must agree with the regexes on:
// the hit and benign corpora, random instances of every rule, the needle
// alternatives those don't reach, case variants that touch both fold bounds
// (A and Z), the Unicode fold spellings, and multi-rule, CRLF and allow-marker
// lines.
func prefilterSeeds() []string {
	var seeds []string
	for _, line := range hitCorpus() {
		seeds = append(seeds, line)
	}
	rng := rand.New(rand.NewSource(2))
	for _, r := range secretscan.Rules() {
		for i := 0; i < 500; i++ {
			line, _ := randomLine(rng, r.Name)
			seeds = append(seeds, line)
		}
	}
	for _, row := range benignCorpus() {
		seeds = append(seeds, row.line)
	}

	for _, p := range []string{"ghp_", "gho_", "ghu_", "ghs_", "ghr_"} {
		seeds = append(seeds, p+strings.Repeat("A1b2", 9))
	}
	seeds = append(seeds, ghFineTok(), "ASIA"+strings.Repeat("Q7", 8))
	for _, c := range "abprs" {
		seeds = append(seeds, "xox"+string(c)+"-"+strings.Repeat("12-", 4))
	}
	seeds = append(seeds,
		"sk-ant-"+strings.Repeat("aB_1", 5),
		"-----BEGIN "+"PRIVATE KEY-----",
		"-----BEGIN "+"EC PRIVATE KEY BLOCK-----",
	)

	for _, w := range keyContextWords {
		for _, v := range []string{w, strings.ToUpper(w), mixedCase(w)} {
			seeds = append(seeds, v+" = "+highEntropy(), `"`+v+`": "`+highEntropy()+`"`)
		}
	}
	v := strings.Repeat("Ab3/", 6)
	seeds = append(seeds,
		"AUTHORIZATION: BEARER "+v,
		"authorization: basic "+v,
		`"AuThOrIzAtIoN"= "Bearer `+v,
	)

	seeds = append(seeds,
		"paſſword = "+highEntropy(),
		"toKen: "+highEntropy(),
		"ſecret = "+highEntropy(),
		"api_Key = "+highEntropy(),
		"acceſſ-key = "+highEntropy(),
	)

	seeds = append(seeds,
		ghTok()+" "+glTok()+" password = "+highEntropy(),
		ghTok()+"\r\n"+awsKey()+"\r\n"+bearer24()+"\r\n",
		ghTok()+" "+secretscan.AllowMarker,
		"clean "+secretscan.AllowMarker+"\n"+skKey(),
		// needle present, regex misses: the regex still has the last word
		"task-list", "ghp_short", "password = short", "PRIVATE KEY alone",
	)
	return seeds
}

// FuzzPrefilterMatchesRegex: no regex matches where its rule's candidate bit
// is clear, and the prefiltered scan equals the every-regex reference.
func FuzzPrefilterMatchesRegex(f *testing.F) {
	for _, s := range prefilterSeeds() {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		mask := secretscan.Candidates(s)
		for i, r := range secretscan.Rules() {
			if mask&(1<<i) == 0 && r.Regex.MatchString(s) {
				t.Errorf("%s: regex matches %q but the prefilter pruned it", r.Name, s)
			}
		}
		got := secretscan.ScanString("f", s)
		want := secretscan.ScanUnfiltered("f", s)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("prefiltered scan of %q = %v, every-regex reference = %v", s, got, want)
		}
	})
}

// TestUnicodeFoldBypass: (?i) folds U+017F onto s and U+212A onto k, so these
// spellings hold no ASCII needle yet satisfy key-context.
func TestUnicodeFoldBypass(t *testing.T) {
	for _, line := range []string{
		"paſſword = " + highEntropy(),
		"toKen: " + highEntropy(),
	} {
		hits := secretscan.ScanString("u", line)
		if len(hits) != 1 || hits[0].Rule != "key-context" {
			t.Errorf("%q: want one key-context hit, got %v", line, hits)
		}
	}
}

// TestCandidatesPrunesNeedleFreeLines: the prefilter is only worth having if
// ordinary lines run no regex at all.
func TestCandidatesPrunesNeedleFreeLines(t *testing.T) {
	for _, line := range []string{
		`func main() { fmt.Println("hi") }`,
		"return nil",
		"[phase]",
		`  id = "cmd-test-duration"`,
		"# A markdown heading",
		"- a bullet with **bold** text",
		"prose with an em-dash — and café", // non-ASCII, but no fold escape
		"",
	} {
		if mask := secretscan.Candidates(line); mask != 0 {
			t.Errorf("%q: want no candidates, got mask %b", line, mask)
		}
	}
}

func TestNoNeedlesFailsOpen(t *testing.T) {
	re := regexp.MustCompile(`z+`)
	rs := []secretscan.Rule{
		secretscan.NewRule("open", re, false),
		secretscan.NewRule("open-fold", re, true),
		secretscan.NewRule("gated", re, false, "zz"),
	}
	for _, line := range []string{"", "abc", "zz"} {
		mask := secretscan.CandidatesIn(rs, line)
		if mask&1 == 0 || mask&2 == 0 {
			t.Errorf("%q: a needle-free rule must always be a candidate, got mask %b", line, mask)
		}
	}
	if mask := secretscan.CandidatesIn(rs, "abc"); mask&4 != 0 {
		t.Errorf("control: a rule whose needle is absent must be pruned, got mask %b", mask)
	}
	if mask := secretscan.CandidatesIn(rs, "zz"); mask&4 == 0 {
		t.Errorf("control: a rule whose needle is present must be a candidate, got mask %b", mask)
	}
}

func TestEveryRuleHasNeedles(t *testing.T) {
	rs := secretscan.Rules()
	if len(rs) > 64 {
		t.Fatalf("%d rules overflow the uint64 candidate mask", len(rs))
	}
	for _, r := range rs {
		if len(secretscan.Needles(r)) == 0 {
			t.Errorf("%s: no needles, so its regex runs on every line", r.Name)
		}
	}
}
