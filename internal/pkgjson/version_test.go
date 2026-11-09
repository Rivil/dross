package pkgjson

import "testing"

func TestTopLevelVersionSpanNonObject(t *testing.T) {
	for _, doc := range []string{`["version", "1.0.0"]`, `"1.0.0"`, `{"version": 1}`} {
		if _, _, ok, err := topLevelVersionSpan([]byte(doc)); ok || err != nil {
			t.Errorf("topLevelVersionSpan(%s) = ok %v err %v, want no version and no error", doc, ok, err)
		}
	}
}
