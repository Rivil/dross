package findings

// Item is one finding from a fresh scan, adapted from a security or quality
// ledger into the shared shape Reconcile consumes. It carries exactly the
// fields that feed the fingerprint (Class is the finding's class or dimension)
// plus the title for display.
type Item struct {
	Class string
	File  string
	Title string
}

// Fingerprint is the item's stable cross-run identity.
func (i Item) Fingerprint() string { return Fingerprint(i.Class, i.File, i.Title) }

// Result partitions a reconciliation's fresh items by how they related to prior
// durable state. After dedup, every fresh item lands in exactly one bucket.
type Result struct {
	New       []Record // never seen before — inserted as tracked
	Folded    []Record // matched a prior dismissed or tracked entry — carried in prior state
	Regressed []Record // matched a prior resolved entry — reappeared (stays resolved, regressed=true)
}

// Reconcile folds a fresh scan's items into the durable store and reports how
// each related to prior state. It is strictly post-scan: it reads items but
// never mutates the input slice, so prior state cannot leak back into the scan
// that produced them. Records for findings absent from this run are left
// untouched — a finding whose file was deleted is retained, not dropped, and
// not marked regressed.
func Reconcile(store *Store, items []Item, runID string) Result {
	var res Result
	seen := map[string]bool{}
	for _, it := range items {
		fp := it.Fingerprint()
		if seen[fp] {
			continue // identical-fingerprint dedup: one durable record per fingerprint
		}
		seen[fp] = true

		prior, ok := store.Get(fp)
		if !ok {
			rec := Record{
				Fingerprint: fp,
				State:       StateTracked,
				Title:       it.Title,
				File:        it.File,
				Class:       it.Class,
				LastRun:     runID,
			}
			store.Put(rec)
			res.New = append(res.New, rec)
			continue
		}

		prior.LastRun = runID
		switch prior.State {
		case StateResolved:
			// A resolved finding that reappears is a regression: the human's
			// resolved decision is preserved, but loudly flagged.
			prior.Regressed = true
			store.Put(prior)
			res.Regressed = append(res.Regressed, prior)
		default:
			// Tracked or dismissed: carry the prior state forward unchanged
			// (a dismissed finding stays out of the "new" surface on re-run).
			store.Put(prior)
			res.Folded = append(res.Folded, prior)
		}
	}
	return res
}
