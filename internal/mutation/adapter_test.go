package mutation

import (
	"reflect"
	"testing"
)

// Supported is what a leg records as its file list, so it must keep the
// scope's order and drop only what the adapter cannot mutate.
func TestSupportedKeepsOrderAndDropsUnsupported(t *testing.T) {
	got := Supported(&Gremlins{}, []string{"a.go", "README.md", "b.go", "assets/prompts/verify.md"})
	want := []string{"a.go", "b.go"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Supported = %v, want %v", got, want)
	}
	if got := Supported(&Gremlins{}, []string{"README.md"}); got != nil {
		t.Fatalf("no supported files must be nil, got %v", got)
	}
}
