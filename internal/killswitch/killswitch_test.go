package killswitch

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), File)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestLoad: the two facts come off a record with every other table present,
// and the error cases keep their kind.
func TestLoad(t *testing.T) {
	const rest = "[thresholds]\n  context_drop = 0.25\n\n[calibration]\n  threshold = 40000\n\n" +
		"[before]\n  method = \"m\"\n\n[[before.task]]\n  phase = \"p\"\n  task = \"t-1\"\n\n"
	row := func(n int) string {
		return fmt.Sprintf("[[after.task]]\n  phase = \"later\"\n  task = \"t-%d\"\n  peak_context = 1\n\n", n)
	}
	cases := []struct {
		name string
		body string
		want Status
	}{
		{name: "pending, no after", body: rest + "[trial]\n  status = \"pending\"\n", want: Status{Trial: "pending"}},
		{name: "pending, two after rows", body: rest + "[trial]\n  status = \"pending\"\n\n" +
			row(1) + row(2),
			want: Status{Trial: "pending", AfterRows: 2}},
		{name: "decided", body: "[trial]\n  status = \"pass\"\n", want: Status{Trial: "pass"}},
		{name: "no trial table", body: rest, want: Status{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Load(write(t, tc.body))
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("Load = %+v, want %+v", got, tc.want)
			}
		})
	}

	t.Run("missing file is not-exist", func(t *testing.T) {
		_, err := Load(filepath.Join(t.TempDir(), File))
		if !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("err = %v, want fs.ErrNotExist", err)
		}
	})

	for name, body := range map[string]string{
		"not toml":           "[trial\n",
		"status not string":  "[trial]\n  status = 1\n",
		"after not an array": "[after]\n  task = \"x\"\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Load(write(t, body))
			if err == nil || !strings.Contains(err.Error(), "does not decode") {
				t.Errorf("err = %v, want one saying the record does not decode", err)
			}
			if errors.Is(err, fs.ErrNotExist) {
				t.Errorf("a malformed record read as a missing one: %v", err)
			}
		})
	}
}

// TestAwaitingAfter: only a pending trial with no [after] row is owed its
// after leg.
func TestAwaitingAfter(t *testing.T) {
	cases := []struct {
		s    Status
		want bool
	}{
		{Status{Trial: StatusPending}, true},
		{Status{Trial: StatusPending, AfterRows: 1}, false},
		{Status{Trial: "pass"}, false},
		{Status{Trial: "fail"}, false},
		{Status{}, false},
	}
	for _, tc := range cases {
		if got := tc.s.AwaitingAfter(); got != tc.want {
			t.Errorf("%+v.AwaitingAfter() = %v, want %v", tc.s, got, tc.want)
		}
	}
}
