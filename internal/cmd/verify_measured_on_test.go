package cmd

import (
	"testing"

	"github.com/Rivil/dross/internal/mutation"
	"github.com/Rivil/dross/internal/remote"
	"github.com/Rivil/dross/internal/verify"
)

// TestMeasuredOnComesFromTheAdapters: the provenance is read off what the run
// is about to USE, never off the grant on disk. Those are different questions —
// a run can hold a grant it was told to ignore, or one it could not reach — and
// answering the second with the first is what makes a local score wear a
// remote label.
func TestMeasuredOnComesFromTheAdapters(t *testing.T) {
	target := &remote.Target{Host: "helicon", Workdir: "/home/rivil/dross"}

	for _, tc := range []struct {
		name     string
		adapters []mutation.Adapter
		want     string
	}{
		{
			name:     "no adapters ran at all",
			adapters: nil,
			want:     "local",
		},
		{
			name:     "every adapter is local",
			adapters: []mutation.Adapter{&mutation.Gremlins{}, &mutation.Stryker{}, &mutation.StrykerNet{}},
			want:     "local",
		},
		{
			name:     "gremlins carries the target",
			adapters: []mutation.Adapter{&mutation.Gremlins{Remote: target}},
			want:     "helicon",
		},
		{
			name:     "stryker carries the target",
			adapters: []mutation.Adapter{&mutation.Stryker{Remote: target}},
			want:     "helicon",
		},
		{
			name:     "stryker-net carries the target",
			adapters: []mutation.Adapter{&mutation.StrykerNet{Remote: target}},
			want:     "helicon",
		},
		{
			// A local adapter beside a remote one is a run measured in TWO
			// places, and the record has to say both. Collapsing it to the
			// host would attribute the local leg's mutants to a machine that
			// never saw them.
			name:     "a local adapter ahead of a remote one",
			adapters: []mutation.Adapter{&mutation.Stryker{}, &mutation.Gremlins{Remote: target}},
			want:     "local, helicon",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := verify.MeasuredOnAdapters(tc.adapters); got != tc.want {
				t.Errorf("MeasuredOnAdapters = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestMeasuredOnIgnoresTheGrant: the discriminator, stated as its own case. A
// repo with a grant on disk whose adapters are local measured locally, and the
// record has to say so — this is the `--skip-mutation` and forced-local shape.
func TestMeasuredOnIgnoresTheGrant(t *testing.T) {
	doctorRemoteFixture(t, "helicon", "/home/rivil/dross", nil)

	// The grant is readable...
	if got := verify.MeasuredOnAdapters(nil); got != "local" {
		t.Errorf("a run with no adapters reported %q despite measuring nothing", got)
	}
	// ...and still does not label a local adapter list as remote.
	if got := verify.MeasuredOnAdapters([]mutation.Adapter{&mutation.Gremlins{}}); got != "local" {
		t.Errorf("a local adapter reported %q — the grant on disk answered for the run", got)
	}
}
