// usedHelper is imported by the entry (src/index.ts), so knip sees it as used.
export function usedHelper(): number {
	return 41 + 1
}

// orphanedWidget is exported but imported by nobody. This is the planted
// dead-code defect: knip(dead-code) reports it as an unused export, while the
// agnostic fallback (scc counts lines, jscpd finds duplication) never does.
export function orphanedWidget(): string {
	return "this export is never imported"
}
