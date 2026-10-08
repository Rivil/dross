package prtriage

import (
	"path/filepath"

	"github.com/Rivil/dross/internal/gitrun"
)

// LoadForPhase reads phase id's triage record from where its verdicts live:
// the working tree when HEAD is phase/<id>, so a resolution not yet committed
// counts, and otherwise the record committed on refs/heads/phase/<id>. A
// branch with no record is an empty one. known is false when the record
// cannot be read — no such branch, or a record that does not decode — and a
// count from it would be a guess.
//
// id comes from a PR's head ref; the working-tree path is built from the
// phase directory's own name (LocalPhase), never from id.
func LoadForPhase(repoDir, id string) (rec Record, known bool) {
	branch := "phase/" + id
	if head, err := gitrun.Trim(repoDir, "symbolic-ref", "HEAD"); err == nil && head == "refs/heads/"+branch {
		local, ok := LocalPhase(repoDir, id)
		if !ok {
			return Record{}, false
		}
		rec, _, err := Load(filepath.Join(repoDir, ".dross", "phases", local, File))
		return rec, err == nil
	}
	ref := "refs/heads/" + branch
	if gitrun.Quiet(repoDir, "rev-parse", "--verify", "--quiet", "--end-of-options", ref) != nil {
		return Record{}, false
	}
	path := ".dross/phases/" + id + "/" + File
	if gitrun.Quiet(repoDir, "cat-file", "-e", "--end-of-options", ref+":"+path) != nil {
		return Record{}, true
	}
	out, err := gitrun.Raw(repoDir, "show", "--end-of-options", ref+":"+path)
	if err != nil {
		return Record{}, false
	}
	//dross:taint-cleared the bytes of pr-triage.toml as committed on the phase branch: a record dross itself wrote, schema-checked by decode, not git's prose
	data := []byte(out)
	rec, err = decode(data)
	return rec, err == nil
}
