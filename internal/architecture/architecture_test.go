package architecture

import (
	"strings"
	"testing"
)

// TestEntryTemplateHasAllFourParts guards entry_template: the micro-template
// must carry all four parts. If any is dropped, this fails (test_contract 2).
func TestEntryTemplateHasAllFourParts(t *testing.T) {
	parts := map[string]string{
		"feature heading":       "### ",
		"one-line description":  "One line:",
		"symbol-link bullet":    ":line",
		"provenance breadcrumb": "introduced",
	}
	for part, marker := range parts {
		if !strings.Contains(EntryTemplate, marker) {
			t.Errorf("entry template missing %s (expected marker %q)", part, marker)
		}
	}
	// The provenance breadcrumb is one compact inline line (provenance_format):
	// it must keep the "·" separators between phase + commit.
	if !strings.Contains(EntryTemplate, "·") {
		t.Error("provenance breadcrumb missing the '·' separator")
	}
}

// TestSkeletonDeclaresFeatureOrganization guards c-1: the seeded doc must say
// it is organized by feature, one entry per capability, never per phase. If
// that contract language is dropped, this fails (test_contract 3).
func TestSkeletonDeclaresFeatureOrganization(t *testing.T) {
	sk := Skeleton()
	for _, want := range []string{
		"organized by feature",
		"per user-facing capability",
		"never one per phase",
	} {
		if !strings.Contains(sk, want) {
			t.Errorf("skeleton missing feature-organization language %q", want)
		}
	}
	// The format must travel with the file — skeleton embeds the template.
	if !strings.Contains(sk, EntryTemplate) {
		t.Error("skeleton must embed EntryTemplate so the format is self-documenting")
	}
}
