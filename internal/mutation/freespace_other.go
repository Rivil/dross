//go:build !unix

package mutation

// freeBytes has no portable implementation here. Reporting "unknown" leaves the
// run exactly as it was before this check existed, which is the documented
// behaviour for a machine that cannot answer.
func freeBytes(string) (uint64, bool) { return 0, false }
